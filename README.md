# ai-gateway

[简体中文](README.zh-CN.md)

A lightweight, configuration-driven HTTP reverse proxy written in Go. It routes requests to statically configured upstreams using a URL label and forwards HTTP traffic without interpreting application payloads.

## Features

- Route requests through configured labels such as /opencode-go/openai/v1/chat/completions.
- Transparently forward methods, paths, query strings, bodies, response status codes, and end-to-end headers.
- Stream responses, including Server-Sent Events (SSE), without buffering the complete response.
- Inject upstream headers and credentials from server-side configuration or environment variables.
- Support hosted services and self-hosted endpoints with the same routing model.
- Enforce request-size and concurrency limits, dial and response-header timeouts, and graceful shutdown.
- Bind to loopback by default. Remote listening requires explicit opt-in and a shared access token.
- No user accounts, database, prompt processing, model parsing, or protocol conversion.

## How routing works

Configure an upstream with a label and base URL:

```yaml
upstreams:
  opencode-go:
    base_url: https://opencode.ai/inference/go
    headers:
      Authorization: "Bearer ${OPENCODE_GO_API_KEY}"

  local-qwen:
    base_url: http://127.0.0.1:8000
```

A request to:

```text
http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions?stream=true
```

is forwarded to:

```text
https://opencode.ai/inference/go/openai/v1/chat/completions?stream=true
```

The route label is removed. The configured base URL path is joined with the remaining request path. The query string and request body are preserved.

The same upstream also exposes the model catalog at:

```text
http://127.0.0.1:8787/opencode-go/v1/models
```

OpenCode Go inference requests require an `X-Opencode-Session` header. Send a stable session ID for each conversation; the gateway forwards it unchanged.

## Quick start

Requires Go 1.25 or later.

1. Copy the example configuration:

   ```sh
   cp configs/config.example.yaml config.yaml
   ```

2. Set the credential used by the hosted upstream:

   ```sh
   export OPENCODE_GO_API_KEY="your-api-key"
   ```

3. Start the gateway:

   ```sh
   go run ./cmd/ai-gateway -config config.yaml
   ```

4. Check the health endpoint:

   ```sh
   curl http://127.0.0.1:8787/healthz
   ```

5. List available OpenCode Go models:

   ```sh
   curl http://127.0.0.1:8787/opencode-go/v1/models
   ```

To send an inference request, use the OpenAI-compatible route and provide a model ID returned by the model catalog:

```sh
curl http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Opencode-Session: ses_replace-with-a-stable-session-id" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Say hello"}]}'
```

The gateway does not implement a model API. The caller is responsible for constructing requests and interpreting responses according to the selected upstream's API.

## Architecture

See the [architecture overview](docs/architecture.md) for the local and trusted internal-network deployment model, request flow, shared upstream credentials, and explicit non-goals. The diagram's Mermaid source is maintained in that document.

## Configuration

See the [configuration guide](docs/configuration.md) for all settings, environment-variable expansion, route behavior, and remote-listener requirements.

The example configuration includes:
- opencode-go: OpenCode Go with a server-side Authorization header.
- local-qwen: a self-hosted HTTP endpoint without a gateway-managed API key.

## Security

The default listener is 127.0.0.1:8787. To listen on a non-loopback address, set `server.allow_remote` to true and configure `server.access_token_env`. The named environment variable must contain a non-empty shared token. Clients send it in `X-Gateway-Token`; the gateway removes that header before forwarding the request upstream.

Use a trusted network boundary and TLS-terminating reverse proxy when exposing the service to other machines. Do not expose an unauthenticated instance to the public internet. See [SECURITY.md](SECURITY.md).

`Proxy-Authorization` is always removed. If an upstream config defines `Authorization`, it replaces any caller-supplied `Authorization` header; otherwise the caller's `Authorization` header is forwarded unchanged. Header values support `${ENV_VAR}` expansion; missing or empty variables cause startup to fail.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/ai-gateway
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Scope and limitations

- Upstream destinations are static configuration. Requests cannot supply arbitrary target URLs.
- The gateway does not parse or modify JSON, prompts, model names, or provider-specific payloads.
- The gateway does not retry requests, balance traffic between replicas, or translate protocols.
- CORS is not enabled by default. Use a same-origin host integration or configure an appropriate trusted front proxy.
- The built-in HTTP server does not terminate TLS. Use a trusted reverse proxy for HTTPS when needed.

## Releases

Version tags matching `v*` trigger cross-platform release builds for Linux, Windows, and macOS on amd64 and arm64. See [the release guide](docs/releasing.md).

## License

MIT. See [LICENSE](LICENSE).
