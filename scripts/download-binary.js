#!/usr/bin/env node
/**
 * Downloads pre-built WhatsApp RPC server binary from GitHub releases.
 * Called automatically during npm postinstall.
 *
 * Skip conditions:
 * - WHATSAPP_RPC_SKIP_BINARY_DOWNLOAD=1
 * - CI=true (CI environments skip download)
 * - Binary already exists
 * - Go is installed and WHATSAPP_RPC_PREFER_SOURCE=1
 */
import { execSync } from 'child_process';
import { createWriteStream, existsSync, mkdirSync, chmodSync, readFileSync, unlinkSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import https from 'https';
import http from 'http';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, '..');
const BIN_DIR = resolve(ROOT, 'bin');

// Read version from package.json
const pkg = JSON.parse(readFileSync(resolve(ROOT, 'package.json'), 'utf-8'));
const VERSION = pkg.version;

// GitHub release URL
const GITHUB_REPO = 'trohitg/whatsapp-rpc';
const BASE_URL = `https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}`;

// Platform detection
function getPlatformInfo() {
  const osMap = { 'win32': 'windows', 'darwin': 'darwin', 'linux': 'linux' };
  const archMap = { 'x64': 'amd64', 'arm64': 'arm64' };

  const os = osMap[process.platform];
  const goarch = archMap[process.arch];

  if (!os || !goarch) {
    return null;
  }

  const ext = process.platform === 'win32' ? '.exe' : '';
  return { os, goarch, ext };
}

// Check if Go is installed
function hasGo() {
  try {
    execSync('go version', { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

// Download file with redirect handling
function downloadFile(url, dest) {
  return new Promise((resolvePromise, reject) => {
    // Remove partial download if exists
    if (existsSync(dest)) {
      try { unlinkSync(dest); } catch { /* ignore */ }
    }

    const request = (currentUrl, redirectCount = 0) => {
      if (redirectCount > 5) {
        reject(new Error('Too many redirects'));
        return;
      }

      const protocol = currentUrl.startsWith('https') ? https : http;

      protocol.get(currentUrl, (response) => {
        // Handle redirects (GitHub releases redirect to CDN)
        if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
          request(response.headers.location, redirectCount + 1);
          return;
        }

        if (response.statusCode === 404) {
          reject(new Error(`Binary not found: v${VERSION} may not have pre-built binaries yet`));
          return;
        }

        if (response.statusCode !== 200) {
          reject(new Error(`HTTP ${response.statusCode}`));
          return;
        }

        const file = createWriteStream(dest);
        const totalBytes = parseInt(response.headers['content-length'], 10);
        let downloadedBytes = 0;

        response.on('data', (chunk) => {
          downloadedBytes += chunk.length;
          if (totalBytes) {
            const percent = ((downloadedBytes / totalBytes) * 100).toFixed(1);
            process.stdout.write(`\r  Downloading: ${percent}%`);
          }
        });

        response.pipe(file);
        file.on('finish', () => {
          file.close();
          console.log(' Done');
          resolvePromise();
        });
        file.on('error', (err) => {
          file.close();
          try { unlinkSync(dest); } catch { /* ignore */ }
          reject(err);
        });
      }).on('error', (err) => {
        try { unlinkSync(dest); } catch { /* ignore */ }
        reject(err);
      });
    };

    request(url);
  });
}

// Main
async function main() {
  // Skip conditions
  if (process.env.WHATSAPP_RPC_SKIP_BINARY_DOWNLOAD === '1') {
    console.log('[whatsapp-rpc] Skipping binary download (WHATSAPP_RPC_SKIP_BINARY_DOWNLOAD=1)');
    return;
  }

  if (process.env.CI === 'true' || process.env.GITHUB_ACTIONS === 'true') {
    console.log('[whatsapp-rpc] Skipping binary download (CI environment)');
    return;
  }

  const platformInfo = getPlatformInfo();
  if (!platformInfo) {
    console.log(`[whatsapp-rpc] Unsupported platform: ${process.platform}/${process.arch}`);
    console.log('[whatsapp-rpc] Please build from source: npm run build');
    return;
  }

  const { os, goarch, ext } = platformInfo;
  const binaryName = `whatsapp-rpc-server-${os}-${goarch}${ext}`;
  const downloadUrl = `${BASE_URL}/${binaryName}`;
  const destPath = resolve(BIN_DIR, `whatsapp-rpc-server${ext}`);

  // Check if binary already exists
  if (existsSync(destPath)) {
    console.log(`[whatsapp-rpc] Binary already exists: ${destPath}`);
    return;
  }

  // Check if Go is installed and user prefers source
  if (hasGo() && process.env.WHATSAPP_RPC_PREFER_SOURCE === '1') {
    console.log('[whatsapp-rpc] Go is installed and WHATSAPP_RPC_PREFER_SOURCE=1, skipping download');
    console.log('[whatsapp-rpc] Build from source with: npm run build');
    return;
  }

  console.log(`[whatsapp-rpc] Downloading pre-built binary...`);
  console.log(`  Version: v${VERSION}`);
  console.log(`  Platform: ${os}/${goarch}`);

  // Create bin directory
  if (!existsSync(BIN_DIR)) {
    mkdirSync(BIN_DIR, { recursive: true });
  }

  // Download binary
  try {
    await downloadFile(downloadUrl, destPath);
  } catch (error) {
    console.error(`\n[whatsapp-rpc] Failed to download binary: ${error.message}`);

    if (hasGo()) {
      console.log('[whatsapp-rpc] Go is installed - you can build from source: npm run build');
    } else {
      console.log('[whatsapp-rpc] Install Go to build from source: https://go.dev/dl/');
    }
    return;
  }

  // Set executable permission (Unix only)
  if (process.platform !== 'win32') {
    try {
      chmodSync(destPath, 0o755);
    } catch (err) {
      console.warn(`[whatsapp-rpc] Warning: Could not set executable permission: ${err.message}`);
    }
  }

  console.log(`[whatsapp-rpc] Binary installed: ${destPath}`);
}

main().catch((err) => {
  // Don't fail npm install - just log and continue
  console.error('[whatsapp-rpc] Binary download error:', err.message);
  console.log('[whatsapp-rpc] WhatsApp features will not work until binary is available.');
  console.log('[whatsapp-rpc] Build from source with: npm run build');
});
