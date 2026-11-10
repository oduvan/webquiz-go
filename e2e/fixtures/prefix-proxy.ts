import http from 'node:http';
import type { AddressInfo } from 'node:net';

export interface PrefixProxy {
  url: string; // the quiz address under the prefix, ending with "/"
  outside: string[]; // requests that did not start with the prefix
  close(): Promise<void>;
}

// Serves a WebQuiz server under /<prefix>/, like the tunnel server serves it under
// {base_url}/{socket_name}/: a request for /<prefix>/x reaches the server as /x.
// Requests outside the prefix get 404 and are recorded. Like the tunnel's nginx, the
// proxy says who the student is in X-Forwarded-For (a public address here).
export async function startPrefixProxy(target: string, prefix: string, clientIP = '203.0.113.7'): Promise<PrefixProxy> {
  const targetURL = new URL(target);
  const outside: string[] = [];
  const server = http.createServer((request, response) => {
    const url = request.url || '/';
    if (url.indexOf('/' + prefix + '/') !== 0) {
      outside.push(url);
      response.writeHead(404, { 'Content-Type': 'text/plain' });
      response.end('outside the prefix');
      return;
    }
    const upstream = http.request(
      {
        host: targetURL.hostname,
        port: targetURL.port,
        method: request.method,
        path: url.substring(prefix.length + 1),
        headers: { ...request.headers, host: targetURL.host, 'x-forwarded-for': clientIP },
      },
      (upstreamResponse) => {
        response.writeHead(upstreamResponse.statusCode || 502, upstreamResponse.headers);
        upstreamResponse.pipe(response);
      },
    );
    upstream.on('error', () => {
      response.writeHead(502, { 'Content-Type': 'text/plain' });
      response.end('the server did not answer');
    });
    request.pipe(upstream);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}/${prefix}/`,
    outside,
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}
