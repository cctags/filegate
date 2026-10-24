// Minimal NUL-framed CDP transport. Uses only Node built-ins.
'use strict';

class CDP {
  constructor(child, timeout = 5000) {
    this.child = child; this.timeout = timeout; this.id = 0;
    this.waiting = new Map(); this.events = []; this.buffer = ''; this.closed = null;
    child.stdio[4].setEncoding('utf8');
    child.stdio[4].on('data', data => {
      if (this.closed) return;
      this.buffer += data;
      for (;;) {
        const end = this.buffer.indexOf('\0'); if (end < 0) break;
        const raw = this.buffer.slice(0, end); this.buffer = this.buffer.slice(end + 1);
        if (!raw) continue;
        let message;
        try { message = JSON.parse(raw); }
        catch { this.close(Error('Invalid CDP response')); return; }
        if (!message || typeof message !== 'object') {
          this.close(Error('Invalid CDP response')); return;
        }
        if (!message.id) { this.events.push(message); continue; }
        const pending = this.waiting.get(message.id);
        if (!pending) continue;
        this.waiting.delete(message.id); clearTimeout(pending.timer);
        message.error ? pending.reject(Error(`CDP error: ${pending.method}`)) : pending.resolve(message.result);
      }
    });
    child.once('error', error => this.close(Error(`Chrome launch failed (${error.code || 'unknown'})`)));
    child.once('exit', (code, signal) => this.close(Error(`Chrome exited (code=${code}, signal=${signal})`)));
    for (const pipe of [child.stdio[3], child.stdio[4]]) {
      pipe.on('error', () => this.close(Error('CDP pipe failed')));
      pipe.on('close', () => this.close(Error('CDP pipe closed')));
    }
    child.stdio[4].on('end', () => this.close(Error('CDP pipe ended')));
    if (child.exitCode !== null || child.signalCode !== null) {
      this.close(Error(`Chrome exited (code=${child.exitCode}, signal=${child.signalCode})`));
    }
  }
  close(error = Error('CDP connection closed')) {
    if (this.closed) return;
    this.closed = error;
    for (const pending of this.waiting.values()) { clearTimeout(pending.timer); pending.reject(error); }
    this.waiting.clear();
  }
  send(method, params = {}, sessionId, timeout = this.timeout) {
    if (this.closed) return Promise.reject(this.closed);
    return new Promise((resolve, reject) => {
      const id = ++this.id;
      const timer = setTimeout(() => {
        this.waiting.delete(id);
        reject(Error(`CDP timeout: ${method} after ${timeout}ms`));
      }, timeout);
      this.waiting.set(id, { resolve, reject, timer, method });
      try {
        this.child.stdio[3].write(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }) + '\0');
      } catch { this.close(Error('CDP pipe write failed')); }
    });
  }
}

// Only startup output is supplied, before browser scenarios issue credentials.
function startupDiagnostics(child, browserOutput, serviceOutput) {
  const redact = text => text
    .replace(/https?:\/\/[^\s"'<>]+/gi, '[URL redacted]')
    .replace(/(?:authorization|cookie|set-cookie)\s*[:=][^\r\n]*/gi, '[header redacted]')
    .replace(/\b(code|token|secret|password)=[^\s]+/gi, '$1=[redacted]')
    .slice(-8192);
  const state = child ? `exit=${child.exitCode ?? 'not exited'}, signal=${child.signalCode ?? 'none'}` : 'not launched';
  return `Chrome startup state: ${state}\n` +
    `Chrome startup stderr:\n${redact(browserOutput) || '(empty)'}\n` +
    `Filegate startup stderr:\n${redact(serviceOutput) || '(empty)'}`;
}

module.exports = { CDP, startupDiagnostics };
