#!/usr/bin/env node
import { program } from 'commander';
import chalk from 'chalk';
import { execa } from 'execa';
import killPort from 'kill-port';
import { Socket } from 'net';
import { execSync, spawn } from 'child_process';
import { existsSync, statSync, mkdirSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const API_PORT = 9400;
const BIN = process.platform === 'win32' ? 'whatsapp-rpc-server.exe' : 'whatsapp-rpc-server';
const BIN_DIR = join(ROOT, 'bin');

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

async function api(foreground = false) {
  if (await portUp(API_PORT)) { log(`API already running on port ${API_PORT}`, 'yellow'); return; }
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

  if (foreground) {
    // Run in foreground - will receive Ctrl+C signals
    const proc = spawn(bin, [], { cwd: ROOT, stdio: 'inherit' });
    proc.on('close', (code) => process.exit(code || 0));
    process.on('SIGINT', () => { proc.kill('SIGINT'); });
    process.on('SIGTERM', () => { proc.kill('SIGTERM'); });
    log(`API running: ws://localhost:${API_PORT}/ws/rpc`, 'green');
  } else {
    // Run detached in background
    spawn(bin, [], { cwd: ROOT, detached: true, stdio: 'ignore' }).unref();
    if (await wait(API_PORT)) log(`API started: ws://localhost:${API_PORT}/ws/rpc`, 'green');
    else log('API failed to start', 'red');
  }
}

async function start() { await api(); }
async function stop() { await kill(API_PORT, 'API'); }

async function status() {
  const a = await portUp(API_PORT);
  log(`API (${API_PORT}): ${a ? 'UP' : 'DOWN'}`, a ? 'green' : 'red');
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

program.name('whatsapp-rpc').version('1.0.0');
program.command('start').description('Start API server').action(start);
program.command('stop').description('Stop API server').action(stop);
program.command('restart').description('Restart API server').action(async () => { await stop(); await sleep(1000); await start(); });
program.command('status').description('Show server status').action(status);
program.command('api').description('Start API server').option('-f, --foreground', 'Run in foreground').action((opts) => api(opts.foreground));
program.command('build').description('Build binary from source (requires Go)').action(build);
program.parse();
