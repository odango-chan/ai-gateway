# Contributing

Contributions are welcome. Keep changes focused and preserve the gateway's role as a small, transparent HTTP reverse proxy.

## Before opening a pull request

- Run gofmt on changed Go files.
- Run go test ./...
- Run go vet ./...
- Add tests for routing, forwarding, streaming, or security behavior changed by the patch.
- Update both English and Chinese documentation when user-visible behavior changes.
- Do not add credentials, request bodies, prompts, or private endpoints to commits or test output.

## Design guidelines

- Keep upstream targets static and configuration-driven.
- Do not parse or transform application payloads.
- Prefer Go's standard HTTP reverse-proxy behavior over custom transport logic.
- Avoid adding databases, user management, provider SDKs, or unrelated service features.
