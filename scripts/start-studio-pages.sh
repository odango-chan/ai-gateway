#!/usr/bin/env bash
# Start the local Gateway for the GitHub Pages Skill Studio example.
# Secrets are never written to this repository or to a config file.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd -- "$script_dir/.." && pwd)"
config="$root/configs/config.studio-pages.example.yaml"

if [[ ! -f "$config" ]]; then
  echo "Missing bundled Studio example config: $config" >&2
  exit 2
fi
if [[ -z "${OPENCODE_GO_API_KEY:-}" ]]; then
  if [[ ! -t 0 ]]; then
    echo "Set OPENCODE_GO_API_KEY in the Gateway process environment (do not commit it)." >&2
    exit 2
  fi
  read -r -s -p "OpenCode Go API key (not saved): " OPENCODE_GO_API_KEY
  printf '\n'
  if [[ -z "$OPENCODE_GO_API_KEY" ]]; then
    echo "A non-empty OpenCode Go API key is required." >&2
    exit 2
  fi
  export OPENCODE_GO_API_KEY
fi

echo "Starting local Gateway at http://127.0.0.1:8787"
echo "Allowed Studio origin: https://odango-chan.github.io"
echo "Press Ctrl+C to stop."
if [[ -x "$root/ai-gateway" ]]; then
  exec "$root/ai-gateway" -config "$config"
elif command -v go >/dev/null 2>&1 && [[ -f "$root/go.mod" ]]; then
  cd "$root"
  exec go run ./cmd/ai-gateway -config "$config"
else
  echo "No Linux/macOS Gateway binary found. Use the matching release asset or install Go to run the source." >&2
  exit 2
fi
