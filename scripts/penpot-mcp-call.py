#!/usr/bin/env python3
"""Minimal streamable-HTTP MCP client for the local Penpot MCP server (bypasses mcp-remote).
Usage: mcpcall.py <tool> [file-with-code | -] ; keeps the session id in a side file."""
import json, sys, os, urllib.request, time
URL = 'http://localhost:4401/mcp'
SID_FILE = os.path.join(os.environ.get("TMPDIR", "/tmp"), "sigame-penpot-mcp-session")
def post(body, sid=None, timeout=120):
    req = urllib.request.Request(URL, data=json.dumps(body).encode(), method='POST')
    req.add_header('Content-Type', 'application/json'); req.add_header('Accept', 'application/json, text/event-stream')
    if sid: req.add_header('mcp-session-id', sid)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            new_sid = r.headers.get('mcp-session-id'); ctype = r.headers.get('Content-Type', ''); raw = r.read().decode()
    except urllib.error.HTTPError as e:
        return None, None, e.read().decode()
    msgs = []
    if 'text/event-stream' in ctype:
        for line in raw.splitlines():
            if line.startswith('data:'):
                try: msgs.append(json.loads(line[5:].strip()))
                except Exception: pass
    elif raw.strip():
        msgs.append(json.loads(raw))
    return new_sid, msgs, raw
def init():
    sid, msgs, raw = post({'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-03-26', 'capabilities': {}, 'clientInfo': {'name': 'sigame-cli', 'version': '0.1'}}})
    if not sid: raise SystemExit('init failed: ' + raw[:300])
    post({'jsonrpc': '2.0', 'method': 'notifications/initialized'}, sid)
    open(SID_FILE, 'w').write(sid); return sid
def call(tool, args, retry=True):
    sid = open(SID_FILE).read().strip() if os.path.exists(SID_FILE) else init()
    _, msgs, raw = post({'jsonrpc': '2.0', 'id': int(time.time() * 1000) % 1000000, 'method': 'tools/call', 'params': {'name': tool, 'arguments': args}}, sid, timeout=180)
    if msgs is None or (not msgs and 'not initialized' in raw.lower()) or ('not initialized' in raw.lower() and retry):
        if retry: init(); return call(tool, args, retry=False)
    for m in msgs or []:
        if 'result' in m:
            for c in m['result'].get('content', []):
                if c.get('type') == 'text': print(c['text'])
            if m['result'].get('isError'): print('[isError]')
            return
        if 'error' in m: print('ERROR', json.dumps(m['error'])[:800]); return
    print('raw:', raw[:800])
if __name__ == '__main__':
    tool = sys.argv[1]
    if tool == 'execute_code':
        src = sys.stdin.read() if len(sys.argv) < 3 or sys.argv[2] == '-' else open(sys.argv[2]).read()
        call('execute_code', {'code': src})
    else:
        call(tool, json.loads(sys.argv[2]) if len(sys.argv) > 2 else {})
