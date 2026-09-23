import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtempSync, mkdirSync, copyFileSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const source = resolve('scripts/setup.mjs');
const example = resolve('wrangler.example.jsonc');

test('first-time deployment creates one D1, reuses it on retry, and refuses unrelated config', async () => {
  const temp = mkdtempSync(join(tmpdir(), 'taskbridge-setup-'));
  const project = join(temp, 'project');
  mkdirSync(join(project, 'scripts'), { recursive: true });
  copyFileSync(source, join(project, 'scripts/setup.mjs'));
  copyFileSync(example, join(project, 'wrangler.example.jsonc'));
  const mockPath = join(temp, process.platform === 'win32' ? 'wrangler.cmd' : 'wrangler');
  const mockScript = process.platform === 'win32' ? join(temp, 'wrangler.js') : mockPath;
  const dbPath = join(temp, 'database.json');
  const logPath = join(temp, 'commands.log');
  writeFileSync(mockScript, `#!/usr/bin/env node
const fs = require('fs');
const args = process.argv.slice(2);
fs.appendFileSync(process.env.MOCK_LOG, args.join(' ') + '\\n');
if (args[0] === 'whoami') { if (process.env.MOCK_REQUIRE_LOGIN === '1' && !fs.existsSync(process.env.MOCK_AUTH)) process.exitCode = 1; else console.log('{}'); }
else if (args[0] === 'login') { if (!args.includes('--device') || !args.includes('--browser=false')) process.exitCode = 2; else fs.writeFileSync(process.env.MOCK_AUTH, 'ok'); }
else if (args[0] === 'd1' && args[1] === 'list') console.log(fs.existsSync(process.env.MOCK_DB) ? fs.readFileSync(process.env.MOCK_DB, 'utf8') : '[]');
else if (args[0] === 'd1' && args[1] === 'create') fs.writeFileSync(process.env.MOCK_DB, JSON.stringify([{ name: args[2], uuid: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' }]));
else if (args[0] === 'deploy') console.log(process.env.MOCK_URL);
else if (args[0] === 'secret') { let input = ''; process.stdin.on('data', chunk => input += chunk); process.stdin.on('end', () => { if (input.length !== 64) process.exitCode = 1; }); }
else if (args[0] === 'd1' && args[1] === 'migrations') console.log('migrations applied');
else process.exitCode = 2;
`, { mode: 0o755 });
  if (process.platform === 'win32') writeFileSync(mockPath, '@echo off\r\nnode "%~dp0wrangler.js" %*\r\n');

  let clients = 0;
  const server = createServer((req, res) => {
    res.setHeader('content-type', 'application/json');
    if (req.url === '/health') { res.end(JSON.stringify({ ok: true, db: 'ok' })); return; }
    if (req.url === '/v1/clients' && req.method === 'POST') {
      clients++;
      res.statusCode = 201;
      res.end(JSON.stringify({ ok: true, token: 'device-secret' }));
      return;
    }
    res.statusCode = 404; res.end('{}');
  });
  await new Promise(done => server.listen(0, '127.0.0.1', done));
  try {
    const url = `http://127.0.0.1:${server.address().port}`;
    const env = { ...process.env, TB_SETUP_TEST_MODE: '1', TB_SETUP_WRANGLER: mockPath,
      NODE_USE_ENV_PROXY: '1', NO_PROXY: 'localhost,127.0.0.1', no_proxy: 'localhost,127.0.0.1',
      TB_NTFY_TOPIC: 'test-topic', TB_SETUP_DB_NAME: 'taskbridge-test', TB_SETUP_WORKER_URL: url,
      MOCK_DB: dbPath, MOCK_LOG: logPath, MOCK_URL: url };
    // Child process needs the HTTP server to keep serving, so use async spawn.
    const { spawn } = await import('node:child_process');
    async function setup(target = project, childEnv = env) {
      return new Promise(resolve => {
        const child = spawn(process.execPath, [join(target, 'scripts/setup.mjs')], { cwd: target, env: childEnv });
        let stderr = '', stdout = '';
        child.stderr.on('data', chunk => stderr += chunk);
        child.stdout.on('data', chunk => stdout += chunk);
        child.on('close', code => resolve({ code, stderr, stdout }));
      });
    }
    let result = await setup();
    assert.equal(result.code, 0, result.stderr);
    assert.equal(clients, 1);
    const state = JSON.parse(readFileSync(join(project, '.local/setup.json')));
    assert.equal(state.databaseId, 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa');
    assert.equal(state.clientToken, 'device-secret');
    assert.equal(JSON.parse(readFileSync(join(project, 'wrangler.jsonc'))).d1_databases[0].database_id, state.databaseId);
    assert.equal(JSON.parse(readFileSync(join(project, '.local/admin.json'))).token, state.adminToken);

    result = await setup();
    assert.equal(result.code, 0, result.stderr);
    assert.equal(clients, 1, 'retry should reuse the existing client token');
    assert.equal(readFileSync(logPath, 'utf8').match(/d1 create/g)?.length, 1, 'retry should not create a second D1');

    writeFileSync(dbPath, '[]');
    result = await setup();
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /not in the current Cloudflare account/);
    assert.equal(readFileSync(logPath, 'utf8').match(/d1 create/g)?.length, 1, 'wrong account must not create a database');
    writeFileSync(dbPath, JSON.stringify([{ name: 'taskbridge-test', uuid: state.databaseId }]));

    rmSync(join(project, '.local'), { recursive: true });
    result = await setup();
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /already exists without setup state/);
    rmSync(join(project, 'wrangler.jsonc'));
    result = await setup();
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /already exists; choose a different/);

    const fresh = join(temp, 'fresh');
    mkdirSync(join(fresh, 'scripts'), { recursive: true });
    copyFileSync(source, join(fresh, 'scripts/setup.mjs'));
    copyFileSync(example, join(fresh, 'wrangler.example.jsonc'));
    const freshEnv = { ...env, TB_NTFY_PREFIX: 'Cospeak3', TB_SETUP_DB_NAME: 'taskbridge-fresh', MOCK_DB: join(temp, 'fresh-db.json') };
    delete freshEnv.TB_NTFY_TOPIC;
    result = await setup(fresh, freshEnv);
    assert.equal(result.code, 0, result.stderr);
    const freshState = JSON.parse(readFileSync(join(fresh, '.local/setup.json')));
    assert.match(freshState.topic, /^Cospeak3-[0-9a-f]{32}$/);
    assert.equal(freshState.topicConfirmed, true);
    assert.match(result.stdout, /subscribe to exactly this topic name/);
    assert.ok(result.stdout.indexOf('Your ntfy topic:') < result.stdout.indexOf('Checking Cloudflare login'));

    const legacy = join(temp, 'legacy');
    mkdirSync(join(legacy, 'scripts'), { recursive: true });
    mkdirSync(join(legacy, '.local'));
    copyFileSync(source, join(legacy, 'scripts/setup.mjs'));
    copyFileSync(example, join(legacy, 'wrangler.example.jsonc'));
    writeFileSync(join(legacy, '.local/setup.json'), JSON.stringify({ databaseName: 'taskbridge-legacy', workerName: 'taskbridge-legacy', adminToken: 'a'.repeat(64), topic: 'Cospeak3', creationStarted: false }));
    const legacyEnv = { ...env, MOCK_DB: join(temp, 'legacy-db.json') };
    delete legacyEnv.TB_NTFY_TOPIC;
    result = await setup(legacy, legacyEnv);
    assert.equal(result.code, 0, result.stderr);
    const legacyState = JSON.parse(readFileSync(join(legacy, '.local/setup.json')));
    assert.match(legacyState.topic, /^Cospeak3-[0-9a-f]{32}$/);
    assert.equal(legacyState.topicConfirmed, true);

    const remote = join(temp, 'remote');
    mkdirSync(join(remote, 'scripts'), { recursive: true });
    copyFileSync(source, join(remote, 'scripts/setup.mjs'));
    copyFileSync(example, join(remote, 'wrangler.example.jsonc'));
    const remoteEnv = { ...env, TB_SETUP_DB_NAME: 'taskbridge-remote', MOCK_DB: join(temp, 'remote-db.json'), MOCK_AUTH: join(temp, 'remote-auth'), MOCK_REQUIRE_LOGIN: '1' };
    result = await setup(remote, remoteEnv);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /device verification URL and short code/);
    assert.match(readFileSync(logPath, 'utf8'), /login --device --browser=false/);

    const unreachable = join(temp, 'unreachable');
    mkdirSync(join(unreachable, 'scripts'), { recursive: true });
    copyFileSync(source, join(unreachable, 'scripts/setup.mjs'));
    copyFileSync(example, join(unreachable, 'wrangler.example.jsonc'));
    const unreachableEnv = { ...env, TB_SETUP_DB_NAME: 'taskbridge-unreachable', TB_SETUP_WORKER_URL: 'http://127.0.0.1:1', MOCK_DB: join(temp, 'unreachable-db.json') };
    result = await setup(unreachable, unreachableEnv);
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /Worker health check request to http:\/\/127\.0\.0\.1:1 failed/);
  } finally {
    await new Promise(done => server.close(done));
    rmSync(temp, { recursive: true, force: true });
  }
});
