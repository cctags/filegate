'use strict';
const assert = require('node:assert/strict');
const { test } = require('node:test');
const { EventEmitter } = require('node:events');
const { PassThrough } = require('node:stream');
const { CDP, startupDiagnostics } = require('./cdp.cjs');

function fixture(t, timeout = 1000) {
  const child = new EventEmitter();
  child.exitCode = null; child.signalCode = null;
  child.stdio = [null, null, null, new PassThrough(), new PassThrough()];
  const cdp = new CDP(child, timeout);
  t.after(() => { cdp.close(); child.stdio[3].destroy(); child.stdio[4].destroy(); });
  return { child, cdp };
}

test('startup handshake has a separate budget from ordinary CDP commands', async t => {
  const { child, cdp } = fixture(t, 10);
  const response = cdp.send('Browser.getVersion', {}, undefined, 1000);
  const timer = setTimeout(() => child.stdio[4].write(JSON.stringify({ id: 1, result: { product: 'delayed browser' } }) + '\0'), 80);
  t.after(() => clearTimeout(timer));
  assert.equal((await response).product, 'delayed browser');
  await assert.rejects(cdp.send('NoResponse'), /CDP timeout: NoResponse after 10ms/);
  assert.equal(cdp.waiting.size, 0);
});

test('a stalled startup remains bounded', async t => {
  const { cdp } = fixture(t);
  await assert.rejects(cdp.send('Browser.getVersion', {}, undefined, 10), /Browser.getVersion after 10ms/);
  assert.equal(cdp.waiting.size, 0);
});

test('fragmented UTF-8, events and out-of-order replies preserve framing', async t => {
  const { child, cdp } = fixture(t);
  const first = cdp.send('First'), second = cdp.send('Second');
  const bytes = Buffer.from(JSON.stringify({ method: 'Event', params: { text: '中文' } }) + '\0' +
    JSON.stringify({ id: 2, result: { value: 2 } }) + '\0' + JSON.stringify({ id: 1, result: { value: 1 } }) + '\0');
  for (const byte of bytes) child.stdio[4].write(Buffer.from([byte]));
  assert.equal((await first).value, 1); assert.equal((await second).value, 2);
  assert.equal(cdp.events[0].params.text, '中文');
});

for (const event of ['end', 'close', 'error', 'exit', 'spawn error']) {
  test(`pending and future requests fail promptly on ${event}`, async t => {
    const { child, cdp } = fixture(t);
    const pending = cdp.send('Browser.getVersion');
    const checked = assert.rejects(pending, /CDP pipe|Chrome exited|Chrome launch failed/);
    if (event === 'exit') child.emit('exit', 1, null);
    else if (event === 'spawn error') child.emit('error', Object.assign(Error('private launcher detail'), { code: 'ENOENT' }));
    else child.stdio[4].emit(event, Error('private pipe detail'));
    await checked;
    await assert.rejects(cdp.send('Next'), /CDP pipe|Chrome exited|Chrome launch failed/);
    assert.equal(cdp.waiting.size, 0);
  });
}

test('already exited browser and invalid messages never echo payloads', async t => {
  const { child, cdp } = fixture(t);
  const pending = cdp.send('Browser.getVersion');
  child.stdio[4].write('private-malformed-response\0');
  await assert.rejects(pending, error => error.message === 'Invalid CDP response');
  child.exitCode = 1;
  const exited = new CDP(child);
  t.after(() => exited.close());
  await assert.rejects(exited.send('Next'), /Chrome exited/);
});

test('startup diagnostics redact URLs, headers and credential fields', () => {
  const output = startupDiagnostics({ exitCode: 1, signalCode: null },
    'failed library\nhttps://user:pass@example.invalid/access?code=private-code\nCookie: private-cookie\nsecret=private-key',
    'Authorization: private-auth\nstarted');
  assert.ok(output.includes('failed library') && output.includes('exit=1'));
  for (const secret of ['user:pass', 'private-code', 'private-cookie', 'private-key', 'private-auth']) assert.equal(output.includes(secret), false);
  assert.ok(startupDiagnostics(null, 'x'.repeat(20000), '').length < 9000);
  assert.ok(startupDiagnostics(null, '', '').includes('not launched'));
});
