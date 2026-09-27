// Static server for the browser arms, plus the report sink: the worker POSTs
// its report to /report, this prints it as an `ARM ` line and exits, and the
// caller then kills the browser. No headless DOM dumping and no virtual-time
// games; a real browser loads a real page.
//
// It also proxies /ch/… to a ClickHouse HTTP endpoint (CH_URL, default
// http://127.0.0.1:8123/), so the page's origin serves the data plane the
// way ADR-0077 SD9 wants it: same origin, no CORS, one TLS authority.
//
//   node serve.mjs <dir> [port]     (port 0 picks one; the chosen port is
//                                    printed first as `PORT <n>`)
import { createServer, request as httpRequest } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';

const dir = process.argv[2];
const port = Number(process.argv[3] || 0);
const types = { '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.wasm': 'application/wasm', '.txt': 'text/plain', '.json': 'application/json' };

const chUrl = new URL(process.env.CH_URL || 'http://127.0.0.1:8123/');

const server = createServer(async (req, res) => {
  process.stderr.write(`${req.method} ${req.url}\n`);
  if (req.url.startsWith('/ch/') || req.url === '/ch') {
    const target = new URL(req.url.replace(/^\/ch\/?/, ''), chUrl);
    const headers = { ...req.headers, host: chUrl.host };
    const up = httpRequest(target, { method: req.method, headers }, (r) => {
      res.writeHead(r.statusCode, r.headers);
      r.pipe(res);
    });
    up.on('error', (e) => { res.writeHead(502).end('proxy: ' + e.message); });
    req.pipe(up);
    return;
  }
  if (req.method === 'POST' && req.url === '/log') {
    let body = '';
    for await (const chunk of req) body += chunk;
    res.writeHead(204).end();
    process.stderr.write('LOG ' + body.trim() + '\n');
    return;
  }
  if (req.method === 'POST' && req.url === '/report') {
    let body = '';
    for await (const chunk of req) body += chunk;
    res.writeHead(204).end();
    process.stdout.write(body.trim() + '\n');
    setTimeout(() => { server.close(); process.exit(0); }, 50);
    return;
  }
  const path = normalize(new URL(req.url, 'http://x').pathname).replace(/^\/+/, '') || 'index.html';
  try {
    const data = await readFile(join(dir, path));
    res.writeHead(200, { 'content-type': types[extname(path)] || 'application/octet-stream', 'cache-control': 'no-store' });
    res.end(data);
  } catch {
    res.writeHead(404).end();
  }
});
server.listen(port, '127.0.0.1', () => { process.stdout.write('PORT ' + server.address().port + '\n'); });
