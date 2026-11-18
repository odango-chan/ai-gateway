# ai-gateway

[简体中文](README.zh-CN.md)

A lightweight, configuration-driven HTTP reverse proxy for routing requests to multiple upstream services.

`ai-gateway` forwards HTTP requests to statically configured upstreams selected by a URL label. It is designed for local use first and can also run as a shared service on a trusted private network.

## Features

- Route requests to configured upstreams using URL path labels.
- Transparently forward request and response bodies, including streaming responses such as Server-Sent Events (SSE).
- Inject upstream headers and credentials from server-side configuration or environment variables.
- Support hosted services and self-hosted endpoints without requiring provider-specific code.
- Apply request-size and concurrency limits, upstream timeouts, and graceful shutdown.
- Bind to loopback by default. Remote listening requires explicit configuration and a shared access token.
- No user accounts, database, prompt processing, model parsing, or protocol conversion.

## Status

The initial implementation is under development. Configuration and API details may change before the first stable release.

## Documentation

- [Configuration and usage](docs/configuration.md)
- [Security model](SECURITY.md)
- [Contributing](CONTRIBUTING.md)

## License

[MIT](LICENSE)
