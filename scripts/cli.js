#!/usr/bin/env node
import { program } from 'commander';
import chalk from 'chalk';
import { execa } from 'execa';
import killPort from 'kill-port';
import { Socket } from 'net';
import { execSync, spawn } from 'child_process';
import { existsSync, statSync, unlinkSync, readdirSync, rmSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const API_PORT = 9400, WEB_PORT = 5000;
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

const py = () => {
  try { execSync('python --version', { stdio: 'ignore' }); return 'python'; }
  catch { try { execSync('python3 --version', { stdio: 'ignore' }); return 'python3'; } catch { return null; } }
};

const hasGo = () => {
  try { execSync('go version', { stdio: 'ignore' }); return true; }
  catch { return false; }
};

async function api(foreground = false) {
  if (await portUp(API_PORT)) { log(`API already on ${API_PORT}`, 'yellow'); return; }
  const bin = join(BIN_DIR, BIN);
  if (!existsSync(bin)) {
    if (!hasGo()) {
      log('Go is not installed. Please either:', 'red');
      log('  1. Install Go: https://go.dev/dl/', 'yellow');
      log('  2. Or run "npm run build" on a machine with Go installed', 'yellow');
      log('  3. Or copy the pre-built binary to: ' + bin, 'yellow');
      process.exit(1);
    }
    log('Building...', 'yellow');
    await build();
  }

  if (foreground) {
    // Run in foreground - will receive Ctrl+C signals
    const proc = spawn(bin, [], { cwd: ROOT, stdio: 'inherit' });
    proc.on('close', (code) => process.exit(code || 0));
    process.on('SIGINT', () => { proc.kill('SIGINT'); });
    process.on('SIGTERM', () => { proc.kill('SIGTERM'); });
    log(`API: ws://localhost:${API_PORT}/ws/rpc`, 'green');
  } else {
    // Run detached in background
    spawn(bin, [], { cwd: ROOT, detached: true, stdio: 'ignore' }).unref();
    if (await wait(API_PORT)) log(`API: ws://localhost:${API_PORT}/ws/rpc`, 'green');
    else log('API failed to start', 'red');
  }
}

async function web() {
  if (await portUp(WEB_PORT)) { log(`Web already on ${WEB_PORT}`, 'yellow'); return; }
  const p = py(); if (!p) { log('Python not found', 'red'); return; }
  if (!await portUp(API_PORT)) log('Warning: API not running', 'yellow');
  spawn(p, ['app.py'], { cwd: join(ROOT, 'web'), detached: true, stdio: 'ignore' }).unref();
  if (await wait(WEB_PORT)) log(`Web: http://localhost:${WEB_PORT}`, 'green');
  else log('Web failed to start', 'red');
}

async function start() { await api(); await sleep(1000); await web(); }
async function stop() { await kill(API_PORT, 'API'); await kill(WEB_PORT, 'Web'); }

async function status() {
  const a = await portUp(API_PORT), w = await portUp(WEB_PORT);
  log(`API (${API_PORT}): ${a ? 'UP' : 'DOWN'}`, a ? 'green' : 'red');
  log(`Web (${WEB_PORT}): ${w ? 'UP' : 'DOWN'}`, w ? 'green' : 'red');
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
    log('Install Go from: https://go.dev/dl/', 'yellow');
    process.exit(1);
  }

  if (!existsSync(BIN_DIR)) { await execa('mkdir', ['-p', BIN_DIR]); }
  await execa('go', ['build', '-o', bin, './src/go/cmd/server'], { cwd: ROOT, stdio: 'inherit' });
  log(`Built: ${BIN} (${(statSync(bin).size / 1024 / 1024).toFixed(1)}MB)`, 'green');
}

async function clean() {
  log('Stopping all processes...', 'blue');
  await stop();
  await sleep(1000);

  // Remove binary
  const bin = join(BIN_DIR, BIN);
  if (existsSync(bin)) { unlinkSync(bin); log(`Removed ${BIN}`, 'green'); }

  // Remove bin directory if empty
  if (existsSync(BIN_DIR) && readdirSync(BIN_DIR).length === 0) {
    rmSync(BIN_DIR, { recursive: true }); log('Removed bin/', 'green');
  }

  // Remove entire data directory (database, QR codes, etc.)
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
  if (existsSync(nodeModules)) { rmSync(nodeModules, { recursive: true }); log('Removed node_modules/', 'green'); }

  // Remove Python cache
  const pycache = join(ROOT, 'web', '__pycache__');
  if (existsSync(pycache)) { rmSync(pycache, { recursive: true }); log('Removed web/__pycache__/', 'green'); }

  // Remove package-lock.json
  const lockFile = join(ROOT, 'package-lock.json');
  if (existsSync(lockFile)) { unlinkSync(lockFile); log('Removed package-lock.json', 'green'); }

  log('Clean complete', 'green');
}

program.name('wa').version('1.0.0');
program.command('start').description('Start all').action(start);
program.command('stop').description('Stop all').action(stop);
program.command('restart').description('Restart all').action(async () => { await stop(); await sleep(1000); await start(); });
program.command('status').description('Status').action(status);
program.command('api').description('Start API only').option('-f, --foreground', 'Run in foreground (receive signals)').action((opts) => api(opts.foreground));
program.command('web').description('Start Web only').action(web);
program.command('build').description('Build binary').action(build);
program.command('clean').description('Clean artifacts').action(clean);
program.parse();
