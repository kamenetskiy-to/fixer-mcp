#!/usr/bin/env node
'use strict';

const http = require('http');

const HOST = '127.0.0.1';
const PORT = 55712;
const POLL_TIMEOUT_MS = 20000;
let nextCursor = 1;
let pendingCommand;
const pollers = new Set();

function json(response, status, value) {
  const body = Buffer.from(JSON.stringify(value));
  response.writeHead(status, {
    'content-type': 'application/json',
    'content-length': body.length,
    'cache-control': 'no-store',
  });
  response.end(body);
}

function safeRelayUrl(value) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'ws:' &&
      (parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost') &&
      parsed.pathname.startsWith('/extension/');
  } catch {
    return false;
  }
}

function deliver(response) {
  json(response, 200, { command: pendingCommand ?? null });
}

function flushPollers() {
  for (const entry of pollers) {
    clearTimeout(entry.timer);
    pollers.delete(entry);
    deliver(entry.response);
  }
}

const server = http.createServer((request, response) => {
  const url = new URL(request.url, `http://${HOST}:${PORT}`);
  if (request.method === 'GET' && url.pathname === '/health') {
    json(response, 200, { status: 'ok', pending: Boolean(pendingCommand) });
    return;
  }
  if (request.method === 'GET' && url.pathname === '/poll') {
    const cursor = Number.parseInt(url.searchParams.get('cursor') || '0', 10);
    if (pendingCommand && pendingCommand.cursor > cursor) {
      deliver(response);
      return;
    }
    const entry = { response };
    entry.timer = setTimeout(() => {
      pollers.delete(entry);
      deliver(response);
    }, POLL_TIMEOUT_MS);
    pollers.add(entry);
    request.once('close', () => {
      clearTimeout(entry.timer);
      pollers.delete(entry);
    });
    return;
  }
  if (request.method === 'POST' && (url.pathname === '/connect' || url.pathname === '/ack')) {
    const chunks = [];
    let size = 0;
    request.on('data', chunk => {
      size += chunk.length;
      if (size > 4096)
        request.destroy();
      else
        chunks.push(chunk);
    });
    request.on('end', () => {
      let body;
      try {
        body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
      } catch {
        json(response, 400, { error: 'invalid JSON' });
        return;
      }
      if (url.pathname === '/connect') {
        if (body.protocolVersion !== 2 || !safeRelayUrl(body.relayUrl)) {
          json(response, 400, { error: 'invalid relay command' });
          return;
        }
        pendingCommand = {
          cursor: nextCursor++,
          relayUrl: body.relayUrl,
          protocolVersion: body.protocolVersion,
        };
        json(response, 202, { cursor: pendingCommand.cursor });
        flushPollers();
        return;
      }
      if (pendingCommand?.cursor === body.cursor)
        pendingCommand = undefined;
      json(response, 200, { acknowledged: body.cursor });
    });
    return;
  }
  json(response, 404, { error: 'not found' });
});

server.listen(PORT, HOST);
