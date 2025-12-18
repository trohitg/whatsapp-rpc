#!/usr/bin/env node
import { program } from 'commander';
import chalk from 'chalk';
import { execa } from 'execa';
import killPort from 'kill-port';
import { Socket } from 'net';
import { execSync, spawn } from 'child_process';
import { existsSync, statSync, unlinkSync, readdirSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const API_PORT = 9400, WEB_PORT = 5000;
const BIN = process.platform === 'win32' ? 'whatsapp-rpc.exe' : 'whatsapp-rpc';

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

async function api() {
  if (await portUp(API_PORT)) { log(`API already on ${API_PORT}`, 'yellow'); return; }
  const bin = join(__dirname, BIN);
  if (!existsSync(bin)) { log('Building...', 'yellow'); await build(); }
  spawn(bin, [], { cwd: __dirname, detached: true, stdio: 'ignore' }).unref();
  if (await wait(API_PORT)) log(`API: ws://localhost:${API_PORT}/ws/rpc`, 'green');
  else log('API failed to start', 'red');
}

async function web() {
  if (await portUp(WEB_PORT)) { log(`Web already on ${WEB_PORT}`, 'yellow'); return; }
  const p = py(); if (!p) { log('Python not found', 'red'); return; }
  if (!await portUp(API_PORT)) log('Warning: API not running', 'yellow');
  spawn(p, ['app.py'], { cwd: join(__dirname, 'web'), detached: true, stdio: 'ignore' }).unref();
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
  const bin = join(__dirname, BIN);
  if (existsSync(bin)) unlinkSync(bin);
  await execa('go', ['build', '-o', BIN, '.'], { cwd: __dirname, stdio: 'inherit' });
  log(`Built: ${BIN} (${(statSync(bin).size / 1024 / 1024).toFixed(1)}MB)`, 'green');
}

async function clean() {
  const bin = join(__dirname, BIN);
  if (existsSync(bin)) { unlinkSync(bin); log(`Removed ${BIN}`, 'green'); }
  readdirSync(__dirname).filter(f => f.startsWith('qr_') && f.endsWith('.png')).forEach(f => {
    unlinkSync(join(__dirname, f)); log(`Removed ${f}`, 'green');
  });
}

program.name('wa').version('1.0.0');
program.command('start').description('Start all').action(start);
program.command('stop').description('Stop all').action(stop);
program.command('restart').description('Restart all').action(async () => { await stop(); await sleep(1000); await start(); });
program.command('status').description('Status').action(status);
program.command('api').description('Start API only').action(api);
program.command('web').description('Start Web only').action(web);
program.command('build').description('Build binary').action(build);
program.command('clean').description('Clean artifacts').action(clean);
program.parse();
