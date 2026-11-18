# Configuration

The gateway reads one YAML file at startup. The default path is `config.yaml`; override it with `-config`.

Start with [the example configuration](../configs/config.example.yaml).

## Server

| Field | Default | Description |
|---|---:|---|
| `server.listen` | `127.0.0.1:8787` | HTTP listen address. |
| `server.allow_remote` | `false` | Explicitly permit a non-loopback listen address. |
| `server.access_token_env` | unset | Environment variable containing the shared gateway token. Required for non-loopback listeners. |

When a shared token is configured, clients send it in `X-Gateway-Token`. The gateway checks it using a constant-time comparison and removes it before forwarding. This is a single shared secret, not a user or identity system.

A non-loopback listener is rejected unless both `allow_remote: true` and `access_token_env` are configured. Use a firewall and a trusted TLS reverse proxy when accessing the service across a network.

## Limits

| Field | Default | Description |
|---|---:|---|
| `limits.max_request_bytes` | `16777216` | Maximum request body size in bytes (16 MiB). |
| `limits.max_concurrent` | `16` | Maximum number of active proxy requests. Excess requests receive HTTP 503. |
| `limits.dial_timeout` | `10s` | Upstream connection establishment timeout. |
| `limits.response_header_timeout` | `120s` | Maximum wait for upstream response headers. This is not a total response timeout; long-lived streams may continue. |
| `limits.idle_conn_timeout` | `90s` | Idle upstream connection timeout. |
| `limits.shutdown_timeout` | `10s` | Graceful shutdown timeout. |

## Upstreams

Each key under `upstreams` is a route label. Labels must be lowercase letters, digits, and hyphens, and must start and end with a letter or digit.

Each upstream supports:

| Field | Required | Description |
|---|---|---|
| `base_url` | yes | Absolute HTTP or HTTPS URL. It may include a path prefix, but not credentials, query parameters, or a fragment. |
| `headers` | no | Static headers to add or replace on requests sent to this upstream. Values support environment-variable expansion. |

For a request to `/opencode-go/openai/v1/chat/completions?stream=true`, with a base URL of `https://opencode.ai/inference/go`, the gateway forwards to `https://opencode.ai/inference/go/openai/v1/chat/completions?stream=true`.

The route label is not forwarded as part of the path. The HTTP method, query string, request body, and normal end-to-end headers are preserved. Standard hop-by-hop headers are handled by Go's reverse proxy implementation.

## OpenCode Go example

OpenCode Go exposes the model catalog and model APIs under different path prefixes. Keep the base URL at `https://opencode.ai/inference/go` and include the API family in the request path:

- Model catalog: `/opencode-go/v1/models` → `/inference/go/v1/models`.
- OpenAI Chat Completions: `/opencode-go/openai/v1/chat/completions` → `/inference/go/openai/v1/chat/completions`.

The configured Authorization header supplies the OpenCode Go API key. Inference requests must also include a stable `X-Opencode-Session` value for the conversation. The gateway forwards this header unchanged; it does not generate or manage conversation IDs.

A real inference request can be sent with:

```sh
curl http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Opencode-Session: ses_replace-with-a-stable-session-id" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Say hello"}]}'
```

## Environment variables and secrets

Header values can reference environment variables using `${NAME}` syntax:

```yaml
upstreams:
  hosted:
    base_url: https://provider.example/v1
    headers:
      Authorization: "Bearer ${PROVIDER_API_KEY}"
```

The variable is expanded when the configuration loads. If a referenced variable is missing or empty, startup fails. Do not commit real keys or put them in the example file.

The `Proxy-Authorization` header is always removed. If an upstream config defines `Authorization`, that configured value replaces the caller's header. Otherwise, the caller's `Authorization` header is forwarded unchanged. Other configured headers replace incoming headers with the same name.

## Routing and health check

- `GET /healthz` returns `ok`.
- All other requests must use `/{label}/{upstream-path}`.
- Unknown labels return HTTP 404.
- The gateway does not inspect request or response bodies and does not implement any model API.
- SSE and other streaming response bodies are copied as they arrive; the complete response is not buffered.

## Example

```sh
export OPENCODE_GO_API_KEY="replace-me"
go run ./cmd/ai-gateway -config configs/config.example.yaml
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/opencode-go/v1/models
curl http://127.0.0.1:8787/local-qwen/v1/models
```

The example local-qwen target assumes a compatible service is listening on `127.0.0.1:8000`.


## Live OpenCode Go smoke test

Start the gateway with `OPENCODE_GO_API_KEY` available in its environment. Then choose a model ID returned by `/opencode-go/v1/models` and run:

```sh
OPENCODE_GO_TEST_MODEL="YOUR_MODEL_ID" ./scripts/smoke-opencode-go.sh
```

The script checks gateway health, confirms the selected model is listed by OpenCode Go, and sends one non-streaming inference request through the gateway. It requires `curl` and `jq`. The inference request may incur provider usage or billing. Set `GATEWAY_URL` for a non-default gateway address, `GATEWAY_TOKEN` when the gateway uses shared-token authentication, or `OPENCODE_GO_SESSION_ID` to provide a stable session ID.
