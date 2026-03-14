/**
 * NimOS — Go Daemon HTTP Proxy
 * 
 * Forwards requests to the Go daemon HTTP API on localhost:5001.
 * If Go daemon is not running, isGoAvailable() returns false
 * so Node.js handles the request as usual. Zero impact when Go is down.
 */

const http = require('http');

const GO_DAEMON_HOST = '127.0.0.1';
const GO_DAEMON_PORT = 5001;

// Routes ported to Go
const GO_ROUTE_PREFIXES = [
  '/api/auth/',
  '/api/user/',
  '/api/users',
  '/api/shares',
  '/api/native-apps',
  '/api/installed-apps',
  '/api/system',
  '/api/cpu',
  '/api/memory',
  '/api/gpu',
  '/api/temps',
  '/api/network',
  '/api/disks',
  '/api/uptime',
  '/api/containers',
  '/api/hostname',
  '/api/hardware/',
  '/api/files',
  '/api/storage',
  '/api/firewall',
  '/api/docker',
  '/api/permissions',
  '/api/ddns',
  '/api/remote-access',
  '/api/ssh',
  '/api/ftp',
  '/api/nfs',
  '/api/dns',
  '/api/certs',
  '/api/proxy',
  '/api/portal',
  '/api/webdav',
  '/api/smb',
  '/api/vms',
];

// ── Health tracking ──
let goAvailable = false;

function checkGoHealth() {
  const req = http.request({
    hostname: GO_DAEMON_HOST,
    port: GO_DAEMON_PORT,
    path: '/api/auth/status',
    method: 'GET',
    timeout: 1000,
  }, (res) => {
    let data = '';
    res.on('data', c => data += c);
    res.on('end', () => {
      const was = goAvailable;
      goAvailable = res.statusCode === 200;
      if (goAvailable && !was) console.log('[go-proxy] Go daemon connected on :' + GO_DAEMON_PORT);
    });
  });
  req.on('error', () => {
    if (goAvailable) console.log('[go-proxy] Go daemon disconnected, using Node.js');
    goAvailable = false;
  });
  req.on('timeout', () => { req.destroy(); goAvailable = false; });
  req.end();
}

// Check on startup, then every 5s
checkGoHealth();
setInterval(checkGoHealth, 5000);

function isGoRoute(url) {
  const path = url.split('?')[0];
  return GO_ROUTE_PREFIXES.some(prefix => path.startsWith(prefix));
}

function proxyToGo(req, res) {
  const chunks = [];
  req.on('data', chunk => chunks.push(chunk));
  req.on('end', () => {
    const body = Buffer.concat(chunks);

    const proxyReq = http.request({
      hostname: GO_DAEMON_HOST,
      port: GO_DAEMON_PORT,
      path: req.url,
      method: req.method,
      headers: {
        'Content-Type': req.headers['content-type'] || 'application/json',
        'Authorization': req.headers['authorization'] || '',
        'X-Forwarded-For': req.socket?.remoteAddress || '',
        'X-Real-IP': req.socket?.remoteAddress || '',
      },
    }, (proxyRes) => {
      const headers = {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
        'Access-Control-Allow-Headers': 'Content-Type, Authorization',
      };
      if (proxyRes.headers['content-type']) headers['Content-Type'] = proxyRes.headers['content-type'];
      if (proxyRes.headers['cache-control']) headers['Cache-Control'] = proxyRes.headers['cache-control'];
      res.writeHead(proxyRes.statusCode, headers);
      proxyRes.pipe(res);
    });

    proxyReq.on('error', (err) => {
      goAvailable = false;
      console.error(`[go-proxy] Request failed: ${err.message}`);
      if (!res.headersSent) {
        res.writeHead(503, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Service temporarily unavailable' }));
      }
    });

    proxyReq.setTimeout(30000, () => {
      proxyReq.destroy();
      if (!res.headersSent) {
        res.writeHead(504, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Request timeout' }));
      }
    });

    if (body.length > 0) proxyReq.write(body);
    proxyReq.end();
  });
}

module.exports = { proxyToGo, isGoRoute, isGoAvailable: () => goAvailable };
