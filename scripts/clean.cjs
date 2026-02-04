#!/usr/bin/env node
// Standalone clean script - no npm dependencies required
// Uses CommonJS for compatibility when node_modules doesn't exist
const { existsSync, unlinkSync, readdirSync, rmSync } = require('fs');
const { join } = require('path');
const { execSync } = require('child_process');

const ROOT = join(__dirname, '..');
const BIN = process.platform === 'win32' ? 'whatsapp-rpc-server.exe' : 'whatsapp-rpc-server';
const BIN_DIR = join(ROOT, 'bin');
const DEFAULT_PORT = 9400;

// Get port from environment or default
const getPort = () => {
  if (process.env.PORT) return parseInt(process.env.PORT, 10);
  if (process.env.WHATSAPP_RPC_PORT) return parseInt(process.env.WHATSAPP_RPC_PORT, 10);
  return DEFAULT_PORT;
};
const API_PORT = getPort();

const log = (msg, color) => {
  const colors = { green: '\x1b[32m', blue: '\x1b[34m', yellow: '\x1b[33m', red: '\x1b[31m', reset: '\x1b[0m' };
  console.log(`${colors[color] || ''}${msg}${colors.reset}`);
};

// Kill process on port
const killPort = (port, name) => {
  try {
    if (process.platform === 'win32') {
      execSync(`for /f "tokens=5" %a in ('netstat -aon ^| findstr :${port} ^| findstr LISTENING') do taskkill /F /PID %a`, { stdio: 'ignore', shell: 'cmd.exe' });
    } else {
      execSync(`lsof -ti:${port} | xargs kill -9 2>/dev/null || true`, { stdio: 'ignore' });
    }
    log(`${name} stopped`, 'green');
  } catch { log(`${name} not running`, 'yellow'); }
};

log('Stopping API server...', 'blue');
killPort(API_PORT, 'API');

// Remove binary
const bin = join(BIN_DIR, BIN);
if (existsSync(bin)) { unlinkSync(bin); log(`Removed ${BIN}`, 'green'); }

// Remove bin directory if empty
if (existsSync(BIN_DIR) && readdirSync(BIN_DIR).length === 0) {
  rmSync(BIN_DIR, { recursive: true }); log('Removed bin/', 'green');
}

// Remove data directory (database, QR codes, etc.)
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

// Remove package-lock.json
const lockFile = join(ROOT, 'package-lock.json');
if (existsSync(lockFile)) { unlinkSync(lockFile); log('Removed package-lock.json', 'green'); }

log('Clean complete', 'green');
