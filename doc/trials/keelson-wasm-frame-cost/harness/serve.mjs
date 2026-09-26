// Static server for the browser arms, plus the report sink: the worker POSTs
// its report to /report, this prints it as an `ARM ` line and exits, and the
// caller then kills the browser. No headless DOM dumping and no virtual-time
// games; a real browser loads a real page.
//
//   node serve.mjs <dir> [port]     (port 0 picks one; the chosen port is
//                                    printed first as `PORT <n>`)
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';

const dir = process.argv[2];
const port = Number(process.argv[3] || 0);
const types = { '.html': 'text/html', '.js': 'text/javascript', '.mjs': 'text/javascript', '.wasm': 'application/wasm', '.txt': 'text/plain', '.json': 'application/json' };

const server = createServer(async (req, res) => {
  process.stderr.write(`${req.method} ${req.url}\n`);
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
