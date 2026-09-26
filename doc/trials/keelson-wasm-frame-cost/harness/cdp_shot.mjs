// Screenshot a page in headless Chromium after real seconds, through the
// DevTools protocol — `--screenshot` fires at the load event, before a worker
// that runs for seconds has posted a frame.
//
//   node cdp_shot.mjs <chromium command...> -- <url> <out.png> [waitMs] [WxH]
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';

const sep = process.argv.indexOf('--');
const cmd = process.argv.slice(2, sep);
//   ... -- <url> <out.png> [waitMs] [WxH] [clickX,clickY]  (a click after the
//   wait, then a further 1.5 s before the capture)
const [url, out, waitArg, sizeArg, clickArg] = process.argv.slice(sep + 1);
const waitMs = Number(waitArg || 15000);
const [w, h] = (sizeArg || '1100x720').split('x').map(Number);

const child = spawn(cmd[0], [...cmd.slice(1), '--headless=new', '--disable-gpu', '--remote-debugging-port=0', `--window-size=${w},${h}`, 'about:blank'], { stdio: ['ignore', 'pipe', 'pipe'] });
let port = null;
const portFound = new Promise((res) => {
  const onData = (d) => { const m = /DevTools listening on ws:\/\/127\.0\.0\.1:(\d+)/.exec(String(d)); if (m && !port) { port = m[1]; res(port); } };
  child.stderr.on('data', onData); child.stdout.on('data', onData);
});
await Promise.race([portFound, new Promise((_, rej) => setTimeout(() => rej(new Error('no DevTools port')), 30000))]);
const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
const page = targets.find((t) => t.type === 'page');
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((res) => { ws.onopen = res; });
let id = 0; const pending = new Map();
ws.onmessage = (e) => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); return; }
  if (m.method === 'Runtime.consoleAPICalled') console.log('console.' + m.params.type + ':', m.params.args.map((a) => a.value ?? a.description).join(' '));
  if (m.method === 'Runtime.exceptionThrown') console.log('exception:', m.params.exceptionDetails.text, m.params.exceptionDetails.exception?.description);
};
const call = (method, params = {}) => new Promise((res) => { const i = ++id; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); });
await call('Page.enable');
await call('Runtime.enable');
await call('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: 1, mobile: false });
await call('Page.navigate', { url });
await new Promise((res) => setTimeout(res, waitMs));
if (clickArg) {
  const [x, y] = clickArg.split(',').map(Number);
  await call('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y });
  await new Promise((res) => setTimeout(res, 150));
  await call('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1 });
  await new Promise((res) => setTimeout(res, 80));
  await call('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1 });
  await new Promise((res) => setTimeout(res, 1500));
}
const status = await call('Runtime.evaluate', { expression: 'document.getElementById("status") ? document.getElementById("status").textContent : ""', returnByValue: true });
const shot = await call('Page.captureScreenshot', { format: 'png' });
writeFileSync(out, Buffer.from(shot.result.data, 'base64'));
console.log('status:', status.result?.result?.value);
console.log('wrote', out);
ws.close(); child.kill();
