#!/usr/bin/env node
import { program } from 'commander';
import chalk from 'chalk';
import { execa } from 'execa';
import killPort from 'kill-port';
import { Socket } from 'net';
import { execSync, spawn } from 'child_process';
import { existsSync, statSync, mkdirSync, rmSync, readdirSync, unlinkSync, readFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const pkg = JSON.parse(readFileSync(join(ROOT, 'package.json'), 'utf8'));
const DEFAULT_PORT = 9400;
const BIN = process.platform === 'win32' ? 'whatsapp-rpc-server.exe' : 'whatsapp-rpc-server';
const BIN_DIR = join(ROOT, 'bin');

// Get port from environment or default
const getPort = (opts = {}) => {
  if (opts.port) return parseInt(opts.port, 10);
  if (process.env.PORT) return parseInt(process.env.PORT, 10);
  if (process.env.WHATSAPP_RPC_PORT) return parseInt(process.env.WHATSAPP_RPC_PORT, 10);
  return DEFAULT_PORT;
};

const log = (m, c = 'blue') => console.log(chalk[c](m));
const sleep = ms => new Promise(r => setTimeout(r, ms));

const portUp = port => new Promise(r => {
  const s = new Socket();
  s.setTimeout(2000);
  s.on('connect', () => { s.destroy(); r(true); });
  s.on('timeout', () => { s.destroy(); r(false); });
  s.on('error', () => r(false));
  s.connect(port, '127.0.0.1');
});

const wait = async (port, ms = 10000) => {
  const t = Date.now();
  while (Date.now() - t < ms) { if (await portUp(port)) return true; await sleep(500); }
  return false;
};

const kill = async (port, name) => {
  try { await killPort(port); log(`${name} stopped`, 'green'); }
  catch { log(`${name} not running`, 'yellow'); }
};

const hasGo = () => {
  try { execSync('go version', { stdio: 'ignore' }); return true; }
  catch { return false; }
};

async function api(opts = {}) {
  const port = getPort(opts);
  const foreground = opts.foreground || false;

  if (await portUp(port)) { log(`API already running on port ${port}`, 'yellow'); return; }
  const bin = join(BIN_DIR, BIN);
  if (!existsSync(bin)) {
    if (!hasGo()) {
      log('Binary not found and Go is not installed.', 'red');
      log('Options:', 'yellow');
      log('  1. Run: npm run postinstall (download pre-built binary)', 'yellow');
      log('  2. Install Go and run: npm run build', 'yellow');
      log('  3. Copy pre-built binary to: ' + bin, 'yellow');
      process.exit(1);
    }
    log('Binary not found, building from source...', 'yellow');
    await build();
  }

  // Pass port to Go binary via environment variable
  const env = { ...process.env, WA_SERVER_PORT: String(port) };

  if (foreground) {
    // Run in foreground - will receive Ctrl+C signals
    const proc = spawn(bin, [], { cwd: ROOT, stdio: 'inherit', env });
    proc.on('close', (code) => process.exit(code || 0));
    process.on('SIGINT', () => { proc.kill('SIGINT'); });
    process.on('SIGTERM', () => { proc.kill('SIGTERM'); });
    log(`API running: ws://localhost:${port}/ws/rpc`, 'green');
  } else {
    // Run detached in background
    spawn(bin, [], { cwd: ROOT, detached: true, stdio: 'ignore', env }).unref();
    if (await wait(port)) log(`API started: ws://localhost:${port}/ws/rpc`, 'green');
    else log('API failed to start', 'red');
  }
}

async function start(opts = {}) { await api(opts); }

async function stop(opts = {}) {
  const port = getPort(opts);
  await kill(port, 'API');
}

async function status(opts = {}) {
  const port = getPort(opts);
  const a = await portUp(port);
  log(`API (${port}): ${a ? 'UP' : 'DOWN'}`, a ? 'green' : 'red');
}

async function build() {
  const bin = join(BIN_DIR, BIN);

  // Skip if binary already exists (e.g., downloaded from GitHub Releases)
  if (existsSync(bin)) {
    log(`Binary already exists: ${BIN} (${(statSync(bin).size / 1024 / 1024).toFixed(1)}MB)`, 'green');
    return;
  }

  // Build from source requires Go
  if (!hasGo()) {
    log('Go is not installed and no pre-built binary found.', 'red');
    log('Options:', 'yellow');
    log('  1. Run: npm run postinstall (download pre-built binary)', 'yellow');
    log('  2. Install Go from: https://go.dev/dl/', 'yellow');
    process.exit(1);
  }

  // Create bin directory if needed
  if (!existsSync(BIN_DIR)) {
    mkdirSync(BIN_DIR, { recursive: true });
  }

  log('Building from source...', 'blue');
  await execa('go', ['build', '-o', bin, './src/go/cmd/server'], { cwd: ROOT, stdio: 'inherit' });
  log(`Built: ${BIN} (${(statSync(bin).size / 1024 / 1024).toFixed(1)}MB)`, 'green');
}

async function clean(opts = {}) {
  const port = getPort(opts);

  // Stop API server
  log('Stopping API server...', 'blue');
  await kill(port, 'API');

  // Remove bin directory
  if (existsSync(BIN_DIR)) {
    rmSync(BIN_DIR, { recursive: true });
    log('Removed bin/', 'green');
  }

  // Remove data directory
  const dataDir = join(ROOT, 'data');
  if (existsSync(dataDir)) {
    rmSync(dataDir, { recursive: true });
    log('Removed data/', 'green');
  }

  // Remove any .db files in project root (legacy locations)
  readdirSync(ROOT).filter(f => f.endsWith('.db') || f.endsWith('.db-wal') || f.endsWith('.db-shm')).forEach(f => {
    unlinkSync(join(ROOT, f));
    log(`Removed ${f}`, 'green');
  });

  // Remove node_modules
  const nodeModules = join(ROOT, 'node_modules');
  if (existsSync(nodeModules)) {
    rmSync(nodeModules, { recursive: true });
    log('Removed node_modules/', 'green');
  }

  // Remove package-lock.json
  const lockFile = join(ROOT, 'package-lock.json');
  if (existsSync(lockFile)) {
    unlinkSync(lockFile);
    log('Removed package-lock.json', 'green');
  }

  log('Clean complete', 'green');
}

// Global port option for all commands
const portOption = ['-p, --port <port>', 'API port (default: 9400, or PORT/WHATSAPP_RPC_PORT env var)'];

program.name('whatsapp-rpc').version(pkg.version);
program.command('start').description('Start API server').option(...portOption).action(start);
program.command('stop').description('Stop API server').option(...portOption).action(stop);
program.command('restart').description('Restart API server').option(...portOption).action(async (opts) => { await stop(opts); await sleep(1000); await start(opts); });
program.command('status').description('Show server status').option(...portOption).action(status);
program.command('api').description('Start API server').option('-f, --foreground', 'Run in foreground').option(...portOption).action(api);
program.command('build').description('Build binary from source (requires Go)').action(build);
program.command('clean').description('Full cleanup (stop server, remove bin/, data/, node_modules/)').action(clean);
program.parse();
