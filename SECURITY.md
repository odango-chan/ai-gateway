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
- Browser CORS is opt-in and exact-origin only. Allowlisting a public GitHub Pages origin is **not user authentication**; do not inject a shared Gateway or provider token into shipped Pages JavaScript.
- Local loopback Gateway use still needs an explicit trusted browser-origin allowlist. Deny unexpected `Origin` traffic to avoid cross-site requests causing model usage even when the response cannot be read.
- For public multi-user access, the current shared-token model is insufficient; retain trusted-network boundaries until server-side identity, authorization and usage limits are implemented.

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub's Report a vulnerability feature for this repository. Do not publish exploit details in a public issue before maintainers have had an opportunity to investigate.
