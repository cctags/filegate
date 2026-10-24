// Optional Chrome/Chromium regression. Uses only Node built-ins and CDP pipes.
'use strict';
require('./timezone.cjs');
const assert = require('node:assert/strict');
const { spawn, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const net = require('node:net');
const http = require('node:http');
const { once } = require('node:events');
const { CDP, startupDiagnostics } = require('./cdp.cjs');

const root = path.resolve(__dirname, '..');
const binary = path.join(root, 'tmp/filegate');
const dir = fs.mkdtempSync(path.join(root, 'tmp/browser-'));
const config = path.join(dir, 'config.json');
const env = { ...process.env, TMPDIR: dir };
let app, chrome, external, cdp;
let appOutput = '', chromeOutput = '';
let browserReady = false;
const browserStartupTimeout = 30000;
const startupOutputLimit = 16384;
const credentials = [];
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(check, description) {
  const end = Date.now() + 10000;
  do { try { const value = await check(); if (value) return value; } catch (err) {
    if (!/context.*destroyed|Cannot find context|Inspected target navigated/i.test(err.message)) throw err;
  } await delay(25); } while (Date.now() < end);
  throw Error(`Timed out: ${description}`);
}
function cli(...args) {
  const out = spawnSync(binary, ['--config', config, ...args], { env, encoding: 'utf8', timeout: 5000 });
  assert.equal(out.status, 0, `CLI ${args[0]} failed: ${out.stderr}`);
  return args[0] === 'init' ? null : JSON.parse(out.stdout);
}
function code(alias) { const issued = cli('code', alias); credentials.push(issued.code); return issued; }
async function freePort() {
  const probe = net.createServer(); probe.listen(0, '127.0.0.1'); await once(probe, 'listening');
  const port = probe.address().port; await new Promise(resolve => probe.close(resolve)); return port;
}
async function stop(child) {
  if (!child || !child.pid || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit'); child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 3000);
  try { await exited; } finally { clearTimeout(timer); }
}

async function scenario(base, landing, javascript) {
  const { browserContextId } = await cdp.send('Target.createBrowserContext');
  try {
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank', browserContextId });
    const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
    const call = (method, params) => cdp.send(method, params, sessionId);
    const evaluate = async expression => {
      const out = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
      if (out.exceptionDetails) throw Error(out.exceptionDetails.text);
      return out.result.value;
    };
    await call('Page.enable'); await call('Network.enable'); await call('Log.enable');
    await call('Emulation.setTimezoneOverride', { timezoneId: 'Asia/Shanghai' });
    await call('Emulation.setScriptExecutionDisabled', { value: !javascript });
    const waitPath = async expected => until(() => evaluate(`document.readyState === 'complete' && location.pathname === ${JSON.stringify(expected)}`), `navigation to ${expected}`);
    const navigate = async (url, expected) => { await call('Page.navigate', { url }); await waitPath(expected); };
    const submit = () => evaluate(`document.querySelector('form').requestSubmit(); true`);
    const first = code('ab');
    await navigate(base + '/', '/');
    await evaluate(`document.querySelector('#code').value = ${JSON.stringify(first.code)}; true`);
    const startEvent = cdp.events.length;
    await submit(); await waitPath('/browse/');
    const post = cdp.events.slice(startEvent).find(e => e.sessionId === sessionId && e.method === 'Network.requestWillBeSent' && e.params.request.method === 'POST' && e.params.request.url === base + '/login');
    assert.ok(post, 'browser did not send login POST');
    const form = new URLSearchParams(post.params.request.postData);
    if (javascript) {
      assert.equal(form.get('client_timezone'), 'Asia/Shanghai');
      assert.equal(form.get('client_utc_offset'), '480');
    } else { assert.equal(form.has('client_timezone'), false, 'disabled script still added timezone'); }
    const extra = await until(() => cdp.events.find(e => e.sessionId === sessionId && e.method === 'Network.requestWillBeSentExtraInfo' && e.params.requestId === post.params.requestId), 'Origin header');
    assert.equal(extra.params.headers.Origin || extra.params.headers.origin, base);
    const cookies = (await call('Network.getCookies', { urls: [base + '/'] })).cookies;
    const session = cookies.find(c => c.name === 'filegate_session');
    assert.ok(session && session.httpOnly && session.sameSite === 'Strict' && !session.secure);
    credentials.push(session.value);
    const beforeInjection = cdp.events.filter(e => e.sessionId === sessionId && e.method === 'Log.entryAdded' && /Content Security Policy|script-src|Refused to execute/i.test(e.params.entry.text));
    assert.equal(beforeInjection.length, 0, 'fixed timezone script violated CSP');
    if (javascript) {
      await evaluate(`(()=>{const s=document.createElement('script');s.textContent='globalThis.filegateUnexpectedScript=true';document.body.appendChild(s);return true})()`);
      assert.equal(await evaluate('globalThis.filegateUnexpectedScript === undefined'), true);
      await until(() => cdp.events.find(e => e.sessionId === sessionId && e.method === 'Log.entryAdded' && /Content Security Policy|script-src|Refused to execute/i.test(e.params.entry.text)), 'CSP blocks unapproved script');
    }
    await navigate(base + '/file/page.html', '/file/page.html');
    assert.equal(await evaluate(`globalThis.filegateFileExecuted === undefined && document.body.innerText.includes('<script>')`), true, 'HTML executed instead of source preview');
    await navigate(base + '/file/image.svg', '/file/image.svg');
    assert.equal(await evaluate('globalThis.filegateFileExecuted === undefined'), true, 'SVG script executed');

    const linked = code('cd'); landing.url = base + '/access?code=' + linked.code;
    // localhost and 127.0.0.1 are different sites, exercising the Lax landing cookie.
    await navigate(landing.origin, '/');
    await evaluate(`document.querySelector('#enter').click(); true`); await waitPath('/access');
    assert.equal(await evaluate('location.search'), '');
    let state = cli('status');
    assert.ok(state.codes.some(c => c.id === linked.id), 'GET consumed link code');
    assert.equal(state.sessions, 1, 'GET changed existing login');
    const pending = (await call('Network.getCookies', { urls: [base + '/access'] })).cookies.find(c => c.name === 'filegate_pending');
    assert.ok(pending && pending.httpOnly && pending.sameSite === 'Lax' && pending.path === '/access');
    credentials.push(pending.value);
    await submit(); await waitPath('/browse/');
    state = cli('status');
    assert.ok(!(state.codes || []).some(c => c.id === linked.id), 'confirmation did not consume code');
    assert.equal(state.sessions, 1, 'confirmation failed to replace existing session');
    assert.equal(await evaluate(`document.body.innerText.includes('only-cd.txt')`), true);
    assert.equal(await evaluate(`document.body.innerText.includes('page.html')`), false);
    await submit(); await waitPath('/');
    const after = (await call('Network.getCookies', { urls: [base + '/'] })).cookies;
    assert.equal(after.some(c => c.name === 'filegate_session'), false, 'logout cookie remained');
    assert.equal(cli('status').sessions, 0);
    console.log(`Browser flow passed: JavaScript ${javascript ? 'enabled' : 'disabled'}, manual login, cross-site link confirmation, file isolation and logout.`);
  } finally { await cdp.send('Target.disposeBrowserContext', { browserContextId }); }
}

(async () => {
  try {
    assert.notEqual(process.getuid(), 0, 'Run browser tests as a non-root user');
    cli('init');
    const port = await freePort(), base = `http://127.0.0.1:${port}`;
    const configText = fs.readFileSync(config, 'utf8').replace('0.0.0.0:17342', `127.0.0.1:${port}`);
    fs.writeFileSync(config, configText);
    fs.writeFileSync(path.join(dir, 'content/ab/page.html'), '<script>globalThis.filegateFileExecuted=true</script>');
    fs.writeFileSync(path.join(dir, 'content/ab/image.svg'), '<svg xmlns="http://www.w3.org/2000/svg"><script>globalThis.filegateFileExecuted=true</script></svg>');
    fs.writeFileSync(path.join(dir, 'content/cd/only-cd.txt'), 'other directory');
    app = spawn(binary, ['--config', config, 'serve'], { env, stdio: ['ignore', 'ignore', 'pipe'] });
    app.on('error', err => { if (!browserReady) appOutput = (appOutput + err.message).slice(-startupOutputLimit); });
    app.stderr.on('data', b => { if (!browserReady) appOutput = (appOutput + b).slice(-startupOutputLimit); });
    await until(async () => { if (app.exitCode !== null) throw Error('Test service failed to start'); try { return (await fetch(base, { signal: AbortSignal.timeout(500) })).ok; } catch { return false; } }, 'service readiness');
    const landing = { url: base };
    external = http.createServer((req, res) => { res.setHeader('Content-Type', 'text/html'); res.end(`<a id="enter" href="${landing.url}">Open access link</a>`); });
    external.listen(0, '127.0.0.1'); await once(external, 'listening'); landing.origin = `http://localhost:${external.address().port}/`;
    chrome = spawn(process.env.CHROME_BIN || 'google-chrome', ['--headless', '--no-sandbox', '--disable-dev-shm-usage', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--remote-debugging-pipe', `--user-data-dir=${path.join(dir, 'chrome')}`, 'about:blank'], { env, stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] });
    chrome.stderr.on('data', b => { if (!browserReady) chromeOutput = (chromeOutput + b).slice(-startupOutputLimit); });
    // Register pipe/exit/error handlers before waiting, including spawn failures.
    cdp = new CDP(chrome);
    const version = await cdp.send('Browser.getVersion', {}, undefined, browserStartupTimeout);
    browserReady = true;
    console.log(`Browser: ${version.product}`);
    for (const javascript of [true, false]) await scenario(base, landing, javascript);
    const log = fs.readFileSync(path.join(dir, 'logs/filegate.log'), 'utf8');
    assert.ok(log.includes('timezone=Asia/Shanghai') && log.includes('utc_offset_minutes=480'));
    for (const credential of credentials) assert.equal(log.includes(credential), false, 'browser credential leaked to application log');
    console.log('Browser CSP, timezone reporting and credential redaction passed.');
  } catch (err) {
    // CDP messages and captured requests can contain codes; never dump them.
    console.error(`Browser regression failed: ${err.message}`);
    if (!browserReady) console.error(startupDiagnostics(chrome, chromeOutput, appOutput));
    console.error(`Artifacts retained in ${dir}`);
    process.exitCode = 1;
  } finally {
    if (cdp) cdp.close();
    await stop(chrome); await stop(app);
    if (external) { external.closeAllConnections(); await new Promise(resolve => external.close(resolve)); }
    if (!process.exitCode) fs.rmSync(dir, { recursive: true, force: true });
  }
})();
