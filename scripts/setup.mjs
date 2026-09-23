#!/usr/bin/env node
// First-time, per-account Cloudflare deployment. Local state permits safe retries.
import { randomBytes } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from 'node:fs';
import { createInterface } from 'node:readline/promises';
import { stdin, stdout } from 'node:process';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const stateDir = join(root, '.local');
const statePath = join(stateDir, 'setup.json');
const configPath = join(root, 'wrangler.jsonc');
const adminPath = join(stateDir, 'admin.json');
const wrangler = process.env.TB_SETUP_WRANGLER || join(root, 'node_modules', '.bin', process.platform === 'win32' ? 'wrangler.cmd' : 'wrangler');
const scopes = ['tasks:write', 'tasks:read', 'notify:write', 'notifications:relay', 'codex:write', 'questions:write', 'questions:read'];
const testMode = process.env.TB_SETUP_TEST_MODE === '1';

function save(file, value) {
  mkdirSync(stateDir, { recursive: true, mode: 0o700 });
  writeFileSync(file, JSON.stringify(value, null, 2) + '\n', { mode: 0o600 });
  if (process.platform !== 'win32') chmodSync(file, 0o600);
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, ...options.env },
    input: options.input,
    stdio: options.capture ? ['pipe', 'pipe', 'pipe'] : [options.input === undefined ? 'inherit' : 'pipe', 'inherit', 'inherit'],
    shell: process.platform === 'win32' && command.endsWith('.cmd'),
  });
  if (result.error || result.status !== 0) {
    if (options.capture && result.stderr) process.stderr.write(result.stderr);
    throw new Error(`${command} ${args.join(' ')} failed: ${result.error?.message || `exit ${result.status}`}`);
  }
  return result.stdout || '';
}

function parseJSON(output, description) {
  try { return JSON.parse(output); }
  catch { throw new Error(`${description} did not return JSON; check Wrangler authentication and version`); }
}

function validateName(name) {
  if (!/^[a-z][a-z0-9-]{2,58}$/.test(name)) throw new Error('D1 name must be 3–59 lowercase letters, digits or hyphens and start with a letter');
  return name;
}

function validateURL(value) {
  const url = new URL(value);
  if (url.protocol !== 'https:' && !(url.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(url.hostname))) {
    throw new Error('Worker URL must use HTTPS');
  }
  return url.origin + url.pathname.replace(/\/$/, '');
}

function workerURL(output) {
  const match = output.replace(/\x1b\[[0-9;]*m/g, '').match(/https:\/\/[^\s"'<>]+\.workers\.dev\/?/);
  return match?.[0]?.replace(/\/$/, '');
}

async function ask(label, fallback) {
  if (fallback) return fallback;
  if (!stdin.isTTY) throw new Error(`${label} is required in noninteractive mode`);
  const rl = createInterface({ input: stdin, output: stdout });
  try { return (await rl.question(`${label}: `)).trim(); }
  finally { rl.close(); }
}

async function main() {
  if (Number(process.versions.node.split('.')[0]) < 22) throw new Error('Node.js 22 or newer is required');
  if (!existsSync(wrangler)) throw new Error('Wrangler is missing; run npm ci in the repository first');
  if (existsSync(configPath) && !existsSync(statePath)) {
    throw new Error('wrangler.jsonc already exists without setup state. Keep the existing deployment; use the manual update commands in README.');
  }
    let state = existsSync(statePath) ? JSON.parse(readFileSync(statePath, 'utf8')) : null;
    if (!state) {
      const topic = await ask('ntfy topic subscribed on your phone', process.env.TB_NTFY_TOPIC);
      if (!topic || !/^[A-Za-z0-9_-]{3,128}$/.test(topic)) throw new Error('ntfy topic must contain 3–128 letters, digits, underscores or hyphens');
      const name = validateName(process.env.TB_SETUP_DB_NAME || `taskbridge-${randomBytes(4).toString('hex')}`);
      const workerName = validateName(process.env.TB_SETUP_WORKER_NAME || name);
      state = { databaseName: name, workerName, adminToken: randomBytes(32).toString('hex'), topic, creationStarted: false };
      save(statePath, state);
    }

    console.log('Checking Cloudflare login...');
    try { run(wrangler, ['whoami', '--json'], { capture: true }); }
    catch {
      if (!stdin.isTTY) throw new Error('Cloudflare login required: run npx wrangler login, then retry');
      console.log('\nCloudflare account setup:');
      console.log('1. Open https://dash.cloudflare.com/sign-up');
      console.log('2. Enter your email and password, then create the account.');
      console.log('3. Open the verification email and verify your address.');
      console.log('4. Return here; Wrangler will open a browser to authorize this computer.');
      await ask('Press Enter after registration or if you already have an account');
      run(wrangler, ['login']);
      run(wrangler, ['whoami', '--json'], { capture: true });
    }

    const databases = parseJSON(run(wrangler, ['d1', 'list', '--json'], { capture: true }), 'D1 list');
    if (!Array.isArray(databases)) throw new Error('D1 list JSON is not an array');
    let database = databases.find(item => item.name === state.databaseName);
    if (state.databaseId && !database) {
      throw new Error(`Saved D1 database ${state.databaseId} is not in the current Cloudflare account; switch accounts before retrying`);
    }
    if (database && !state.creationStarted && !state.databaseId) {
      throw new Error(`D1 database ${state.databaseName} already exists; choose a different TB_SETUP_DB_NAME`);
    }
    if (!database) {
      console.log(`Creating D1 database ${state.databaseName}...`);
      state.creationStarted = true;
      save(statePath, state);
      run(wrangler, ['d1', 'create', state.databaseName]);
      const updated = parseJSON(run(wrangler, ['d1', 'list', '--json'], { capture: true }), 'D1 list');
      database = updated.find(item => item.name === state.databaseName);
    }
    if (!database?.uuid || (state.databaseId && state.databaseId !== database.uuid)) {
      throw new Error('D1 database ID is missing or differs from saved setup state; stopping before migration');
    }
    state.databaseId = database.uuid;
    save(statePath, state);

    if (!existsSync(configPath)) {
      const config = JSON.parse(readFileSync(join(root, 'wrangler.example.jsonc'), 'utf8'));
      config.name = state.workerName;
      config.d1_databases[0].database_name = state.databaseName;
      config.d1_databases[0].database_id = state.databaseId;
      writeFileSync(configPath, JSON.stringify(config, null, 2) + '\n', { flag: 'wx', mode: 0o600 });
    } else {
      const config = JSON.parse(readFileSync(configPath, 'utf8'));
      if (config.name !== state.workerName || config.d1_databases?.[0]?.database_id !== state.databaseId || config.d1_databases?.[0]?.database_name !== state.databaseName) {
        throw new Error('wrangler.jsonc points to another Worker or database');
      }
    }

    console.log('Applying D1 migrations...');
    run(wrangler, ['d1', 'migrations', 'apply', state.databaseName, '--remote'], { env: { CI: '1' } });
    console.log('Setting the Worker admin secret...');
    run(wrangler, ['secret', 'put', 'TB_ADMIN_TOKEN'], { input: state.adminToken });
    console.log('Deploying Worker...');
    const deployOutput = run(wrangler, ['deploy'], { capture: true });
    process.stdout.write(deployOutput);
    const url = validateURL(await ask('Worker URL', process.env.TB_SETUP_WORKER_URL || state.workerURL || workerURL(deployOutput)));
    state.workerURL = url;
    save(statePath, state);

    const health = await fetch(`${url}/health`);
    const healthBody = await health.json().catch(() => ({}));
    if (!health.ok || healthBody.db !== 'ok') throw new Error(`Worker health check failed at ${url}/health`);
    save(adminPath, { url, token: state.adminToken, ntfy_topic: state.topic, heartbeat_seconds: 120 });

    if (!state.clientToken) {
      const clientName = process.env.TB_SETUP_CLIENT_NAME || `setup-${process.env.USER || process.env.USERNAME || 'computer'}`;
      const response = await fetch(`${url}/v1/clients`, {
        method: 'POST',
        headers: { authorization: `Bearer ${state.adminToken}`, 'content-type': 'application/json' },
        body: JSON.stringify({ name: clientName, scopes }),
      });
      const body = await response.json();
      if (!response.ok || !body.token) throw new Error(`Client creation failed: HTTP ${response.status}`);
      state.clientToken = body.token;
      save(statePath, state);
    }

    console.log(`Worker: ${url}\nD1: ${state.databaseName}\nAdmin config: ${adminPath}`);
    if (!testMode) {
      const env = { TB_WORKER_URL: url, TB_CLIENT_TOKEN: state.clientToken, TB_NTFY_TOPIC: state.topic };
      if (process.platform === 'win32') {
        run('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', join(root, 'scripts', 'install.ps1'), '-WorkerUrl', url], { env });
      } else {
        run('bash', [join(root, 'scripts', 'install.sh')], { env });
      }
    }
    console.log('Setup complete. Other computers need their own client token; create one using the saved admin config.');
}

main().catch(error => { console.error(`TaskBridge setup: ${error.message}`); process.exitCode = 1; });
