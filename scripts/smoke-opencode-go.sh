#!/usr/bin/env bash
set -euo pipefail

gateway_url="${GATEWAY_URL:-http://127.0.0.1:8787}"
model="${OPENCODE_GO_TEST_MODEL:?Set OPENCODE_GO_TEST_MODEL to a model ID returned by the model catalog}"
session_id="${OPENCODE_GO_SESSION_ID:-ses_ai_gateway_smoke_$(date +%s)}"
gateway_token="${GATEWAY_TOKEN:-}"

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

curl_args=(--silent --show-error --fail-with-body --connect-timeout 10 --max-time 180)
if [[ -n "$gateway_token" ]]; then
  curl_args+=(-H "X-Gateway-Token: $gateway_token")
fi

curl "${curl_args[@]}" "$gateway_url/healthz" >/dev/null
echo "Gateway health: PASS"

models="$(curl "${curl_args[@]}" "$gateway_url/opencode-go/v1/models")"
if ! printf '%s' "$models" | jq -e --arg id "$model" 'any(.data[]?; .id == $id)' >/dev/null; then
  echo "Model '$model' was not found in the OpenCode Go model catalog." >&2
  exit 1
fi
echo "OpenCode Go model catalog: PASS ($model is available)"

payload="$(jq -cn --arg model "$model" '{
  model: $model,
  messages: [{role: "user", content: "Reply with exactly: ai-gateway-smoke-ok"}],
  max_tokens: 32,
  stream: false
}')"

response="$(curl "${curl_args[@]}" -H "Content-Type: application/json" -H "X-Opencode-Session: $session_id" --data "$payload" "$gateway_url/opencode-go/openai/v1/chat/completions")"

reply="$(printf '%s' "$response" | jq -er '.choices[0].message.content | select(type == "string" and length > 0)')"
printf 'OpenCode Go Chat Completions: PASS\n'
printf 'Model: %s\n' "$model"
printf 'Session: %s\n' "$session_id"
printf 'Reply: %s\n' "$reply"
