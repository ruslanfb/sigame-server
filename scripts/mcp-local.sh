#!/usr/bin/env bash
# Starts the local MCP helpers used while developing the web client:
#   * Penpot MCP  (official @penpot/mcp): plugin on :4400, MCP on http://localhost:4401/mcp
#   * Serena      (oraios/serena):        MCP on http://localhost:14181/mcp
#   * Chrome with remote debugging on :9222 for the Playwright MCP
# Usage: scripts/mcp-local.sh [start|stop|status]   (logs in ~/.sigame-mcp)
set -euo pipefail
export PATH="/opt/homebrew/bin:$HOME/.local/bin:$PATH"
DIR="$HOME/.sigame-mcp"; mkdir -p "$DIR"
PROJECT="$(cd "$(dirname "$0")/.." && pwd)"
CHROME_PROFILE="$HOME/.sigame-chrome-profile"
PENPOT_MCP_VERSION="2.15.4"
PENPOT_SRC="$DIR/src/penpot-mcp"

port_open() { curl -s -m 3 -o /dev/null "http://localhost:$1/" 2>/dev/null; }  # localhost: the Penpot MCP binds ::1

start() {
  if ! port_open 4401; then
    # npx @penpot/mcp runs "corepack pnpm run bootstrap", which pnpm 10 aborts because
    # esbuild/sharp need build scripts; keep a stable checkout with the builds approved.
    if [ ! -d "$PENPOT_SRC" ]; then
      echo "→ installing @penpot/mcp@$PENPOT_MCP_VERSION into $PENPOT_SRC"
      mkdir -p "$(dirname "$PENPOT_SRC")" && (cd "$(dirname "$PENPOT_SRC")" && npm pack "@penpot/mcp@$PENPOT_MCP_VERSION" >/dev/null && tar xzf "penpot-mcp-$PENPOT_MCP_VERSION.tgz" && mv package "$(basename "$PENPOT_SRC")" && rm -f "penpot-mcp-$PENPOT_MCP_VERSION.tgz")
      python3 - "$PENPOT_SRC/package.json" <<'PY'
import json, sys
p = sys.argv[1]; d = json.load(open(p)); d.setdefault("pnpm", {})["onlyBuiltDependencies"] = ["esbuild", "sharp"]
json.dump(d, open(p, "w"), indent=2)
PY
      cp -f "$PENPOT_SRC/pnpm-lock.dist.yaml" "$PENPOT_SRC/pnpm-lock.yaml" 2>/dev/null || true
      # pnpm 10 reads the allow-list from pnpm-workspace.yaml in a workspace.
      sed -i '' 's/^  esbuild: set this to true or false$/  esbuild: true/; s/^  sharp: set this to true or false$/  sharp: true/' "$PENPOT_SRC/pnpm-workspace.yaml"
      grep -q onlyBuiltDependencies "$PENPOT_SRC/pnpm-workspace.yaml" 2>/dev/null || printf '\nonlyBuiltDependencies:\n  - esbuild\n  - sharp\n' >> "$PENPOT_SRC/pnpm-workspace.yaml"
    fi
    echo "→ Penpot MCP (corepack pnpm run bootstrap) … log: $DIR/penpot-mcp.log"
    (cd "$PENPOT_SRC" && nohup corepack pnpm run bootstrap > "$DIR/penpot-mcp.log" 2>&1 & echo $! > "$DIR/penpot-mcp.pid")
  else echo "✓ Penpot MCP already on :4401"; fi
  if ! port_open 14181; then
    echo "→ Serena … log: $DIR/serena.log"
    nohup uvx --from git+https://github.com/oraios/serena serena start-mcp-server \
      --transport streamable-http --host 127.0.0.1 --port 14181 --project "$PROJECT" \
      > "$DIR/serena.log" 2>&1 & echo $! > "$DIR/serena.pid"
  else echo "✓ Serena already on :14181"; fi
  if ! curl -s -m 3 http://127.0.0.1:9222/json/version >/dev/null 2>&1; then
    echo "→ Chrome with --remote-debugging-port=9222 (separate profile $CHROME_PROFILE)"
    mkdir -p "$CHROME_PROFILE"
    open -na "Google Chrome" --args --remote-debugging-port=9222 --user-data-dir="$CHROME_PROFILE" --no-first-run --no-default-browser-check "about:blank"
  else echo "✓ Chrome CDP already on :9222"; fi
  echo
  echo "Penpot: open http://192.168.10.217:9001, a file → Plugins → install http://localhost:4400/manifest.json → 'Connect to MCP server'. Keep the plugin panel open."
}

stop() {
  for n in penpot-mcp serena; do
    if [ -f "$DIR/$n.pid" ]; then kill "$(cat "$DIR/$n.pid")" 2>/dev/null && echo "stopped $n" || true; rm -f "$DIR/$n.pid"; fi
  done
  pkill -f "remote-debugging-port=9222" 2>/dev/null && echo "stopped Chrome (CDP)" || true
}

status() {
  for p in 4400 4401 14181 9222; do printf "localhost:%-6s " "$p"; port_open "$p" && echo open || echo closed; done
}

case "${1:-start}" in start) start ;; stop) stop ;; status) status ;; *) echo "usage: $0 [start|stop|status]"; exit 2 ;; esac
