# Security Policy

## Supported versions

Security fixes are provided for the latest development line. Until a stable release is published, users should run the latest reviewed commit.

## Deployment guidance

- Keep the listener on 127.0.0.1 unless remote access is required.
- A non-loopback listener requires explicit opt-in and a shared gateway token.
- Restrict network access with host and network firewalls.
- Use a trusted TLS-terminating reverse proxy when traffic crosses an untrusted network.
- Keep upstream credentials in environment variables or a protected secret store.
- Do not commit real credentials or enable verbose logging of request data.
- Treat access to the gateway as access to every configured upstream.

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub's Report a vulnerability feature for this repository. Do not publish exploit details in a public issue before maintainers have had an opportunity to investigate.
