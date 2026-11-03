// Exercise the packaged executable, directly or through an explicit QEMU runner.
'use strict';
const assert = require('node:assert/strict');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const path = require('node:path');
const { once } = require('node:events');

const [binary, config, ...runner] = process.argv.slice(2);
assert.ok(binary && config, 'Expected binary and initialized test configuration');
assert.notEqual(process.getuid(), 0, 'Run release smoke tests as a non-root user');
const command = runner.length ? runner[0] : binary;
const prefix = runner.length ? [...runner.slice(1), binary] : [];
const directory = path.dirname(config);
let app, output = '', spawnFailed = false, phase = 'startup';
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
function cli(...args) {
  const result = spawnSync(command, [...prefix, '--config', config, ...args], {
    encoding: 'utf8', timeout: 15000, maxBuffer: 1 << 20,
  });
  // Never print captured output: administrative responses contain credentials.
  assert.equal(result.status, 0, `Release CLI ${args[0]} failed`);
  return JSON.parse(result.stdout);
}
async function until(check, description) {
  const deadline = Date.now() + 15000;
  do {
    assert.ok(!spawnFailed, 'Release service could not start');
    assert.ok(app && app.exitCode === null && app.signalCode === null, 'Release service exited unexpectedly');
    if (await check()) return;
    await delay(50);
  } while (Date.now() < deadline);
  throw Error(`Timed out: ${description}`);
}
async function freePort() {
  const probe = net.createServer();
  probe.listen(0, '127.0.0.1');
  try {
    await once(probe, 'listening');
    return probe.address().port;
  } finally {
    if (probe.listening) await new Promise(resolve => probe.close(resolve));
  }
}
function request(base, route, cookie = '', form) {
  return new Promise((resolve, reject) => {
    const body = form ? new URLSearchParams(form).toString() : '';
    const req = http.request(base + route, {
      method: form ? 'POST' : 'GET', agent: false,
      headers: { Cookie: cookie, Origin: base,
        ...(form ? { 'Content-Type': 'application/x-www-form-urlencoded', 'Content-Length': Buffer.byteLength(body) } : {}) },
    }, res => {
      let text = '';
      res.setEncoding('utf8');
      res.on('error', reject);
      res.on('data', chunk => {
        text += chunk;
        if (text.length > (1 << 20)) req.destroy(Error('Release response exceeded limit'));
      });
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, text }));
    });
    req.setTimeout(5000, () => req.destroy(Error('Release HTTP request timed out')));
    req.on('error', reject);
    req.end(body);
  });
}
function start() {
  output = ''; spawnFailed = false;
  app = spawn(command, [...prefix, '--config', config, 'serve'], { stdio: ['ignore', 'ignore', 'pipe'] });
  app.on('error', () => { spawnFailed = true; });
  app.stderr.on('data', chunk => { output = (output + chunk).slice(-16384); });
}
async function stop() {
  if (!app || !app.pid || app.exitCode !== null || app.signalCode !== null) return;
  const child = app;
  const exited = new Promise(resolve => child.once('exit', resolve));
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 12000);
  try {
    await exited;
    assert.equal(child.exitCode, 0, 'Release service did not shut down gracefully');
  } finally { clearTimeout(timer); }
}

(async () => {
  try {
    const port = await freePort(), base = `http://127.0.0.1:${port}`;
    const original = fs.readFileSync(config, 'utf8');
    assert.ok(original.includes('0.0.0.0:17342'), 'Unexpected generated listener');
    const configured = original.replace('0.0.0.0:17342', `127.0.0.1:${port}`);
    fs.writeFileSync(config, configured);
    fs.writeFileSync(path.join(directory, 'content/ab/hello.txt'), 'release hello');
    fs.writeFileSync(path.join(directory, 'content/cd/secret.txt'), 'other directory');
    fs.symlinkSync('../cd/secret.txt', path.join(directory, 'content/ab/escape.txt'));
    start();
    await until(async () => {
      try { return (await request(base, '/')).status === 200; } catch { return false; }
    }, 'HTTP listener');
    phase = 'login and isolation';
    const issued = cli('code', 'ab');
    const durable = cli('code', 'ab');
    cli('block', '192.0.2.1');
    const login = await request(base, '/login', '', { code: issued.code });
    assert.equal(login.status, 303, 'Release login failed');
    const session = (login.headers['set-cookie'] || []).find(value => value.startsWith('filegate_session='));
    assert.ok(session, 'Release login did not establish a session');
    const cookie = session.split(';', 1)[0];
    const listing = await request(base, '/browse/', cookie);
    assert.equal(listing.status, 200);
    assert.ok(listing.text.includes('hello.txt') && !listing.text.includes('secret.txt'), 'Directory listing isolation failed');
    const file = await request(base, '/file/hello.txt', cookie);
    assert.equal(file.status, 200);
    assert.equal(file.text, 'release hello');
    for (const route of ['/file/secret.txt', '/file/escape.txt', '/file/%2e%2e/cd/secret.txt']) {
      assert.equal((await request(base, route, cookie)).status, 404, 'Release file isolation failed');
    }
    assert.equal((await request(base, '/login', '', { code: issued.code })).status, 401, 'Code was reusable');
    assert.equal(cli('status').sessions, 1);
    phase = 'configuration reload';
    // A successful signal send is not enough: observe changed mapping behavior.
    fs.mkdirSync(path.join(directory, 'content/new'));
    fs.writeFileSync(path.join(directory, 'content/new/changed.txt'), 'reloaded directory');
    assert.ok(configured.includes('"ab": "ab"'), 'Unexpected generated directory mapping');
    fs.writeFileSync(config, configured.replace('"ab": "ab"', '"ab": "new"'));
    app.kill('SIGHUP');
    await until(() => output.includes('configuration reloaded'), 'configuration reload acknowledgement');
    assert.equal((await request(base, '/browse/', cookie)).status, 303, 'Reload retained old session');
    const afterReload = cli('status');
    assert.ok(!(afterReload.codes || []).some(code => code.id === durable.id), 'Reload retained old mapping code');
    const retained = cli('code', 'ab');
    const authenticated = await request(base, '/login', '', { code: retained.code });
    assert.equal(authenticated.status, 303);
    const newCookie = authenticated.headers['set-cookie'].find(value => value.startsWith('filegate_session=')).split(';', 1)[0];
    assert.equal((await request(base, '/file/changed.txt', newCookie)).status, 200);
    assert.equal((await request(base, '/file/hello.txt', newCookie)).status, 404);
    phase = 'durable restart';
    const pending = cli('code', 'ab');
    await stop();
    start();
    await until(async () => {
      try { return (await request(base, '/')).status === 200; } catch { return false; }
    }, 'restarted HTTP listener');
    const restored = cli('status');
    assert.equal(restored.sessions, 0, 'Restart restored a memory-only session');
    assert.ok((restored.codes || []).some(code => code.id === pending.id), 'Restart lost an unused code');
    assert.ok(!(restored.codes || []).some(code => code.id === retained.id), 'Restart restored a consumed code');
    assert.ok((restored.blacklist || []).includes('192.0.2.1/32'), 'Restart lost the blacklist');
    assert.equal((await request(base, '/file/changed.txt', newCookie)).status, 303);
    assert.equal((await request(base, '/login', '', { code: pending.code })).status, 303);
    await stop();
    console.log('Release runtime passed: CLI, login, file isolation, reload, graceful shutdown and durable restart.');
  } catch {
    // Assertions, HTTP bodies and CLI output can contain credentials: keep errors fixed.
    console.error(`Release runtime smoke failed during ${phase} (credential-bearing diagnostics withheld).`);
    process.exitCode = 1;
  } finally {
    try { await stop(); } catch { process.exitCode = 1; }
  }
})();
