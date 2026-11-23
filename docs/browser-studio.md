# Browser-hosted Skill Studio + Go Gateway

Status: deployment recipe for GitHub Pages Studio; not an assertion that a
live Provider request or a full AI Editor Tab Apply was qualified.

## Boundaries

```text
GitHub Pages (static assets only)
    Studio Authoring Core / browser extension host
    Compiler v2 Go/WASM Worker
                |
                | browser fetch; origin + possible Local Network Access permission
                v
Go AI Gateway (user-managed loopback OR trusted private network)
                |
                | Provider API Key injected only by Gateway
                v
OpenCode Go or another OpenAI-compatible model service
```

GitHub Pages cannot host the Go Gateway runtime, listen for requests, or hold
a private deployment-wide API key. This model keeps Pages static and Gateway
independent. No Cloudflare Worker, second Compiler, browser WASM HTTP server,
or model protocol translator is required.

## Single-user local / WSL deployment

1. Start Go Gateway on the workstation with an explicitly chosen upstream and
   provider API credential available **only** to the Gateway process.
2. Keep `server.listen: "127.0.0.1:8787"` (do not use `0.0.0.0` for this
   profile). Add the exact Pages **origin**, not the project path:

   ```yaml
   server:
     listen: "127.0.0.1:8787"
     cors_allowed_origins:
       - "https://odango-chan.github.io"
   ```

3. For an OpenCode Go route, verify local health and model catalog from the
   gateway's host:

   ```sh
   curl -fsS http://127.0.0.1:8787/healthz
   curl -fsS http://127.0.0.1:8787/opencode-go/v1/models
   ```

4. Configure Studio's application settings (not its source repository):
   `connectionAgent.skillStudio.ai.gatewayUrl` =
   `http://127.0.0.1:8787/opencode-go/openai` and
   `connectionAgent.skillStudio.ai.model` = an actual ID accepted by the
   upstream. The Client appends `/v1/chat/completions`. It supplies a stable
   `X-Opencode-Session` and never needs the Provider API Key.
5. From a trusted Pages browser profile, verify the preflight and actual
   Model Proposal via the Studio Editor Tab. Browser HTTPS-to-loopback access
   is browser-dependent; Local Network Access prompts, loopback mapping from
   WSL, private-network fetch restrictions, and mixed-content rules can block
   it even with correct Gateway CORS. Do not weaken security to work around
   browser denials.

Local WSL networking varies by Windows/WSL configuration. Verify that the
**browser on Windows**, not just `curl` inside WSL, can reach the exact loopback
endpoint. If it cannot, use an appropriately protected same-device bridge or
trusted HTTPS endpoint rather than publishing a loopback secret as a public
address.

## Minimal preflight acceptance

```sh
curl -i -X OPTIONS 'http://127.0.0.1:8787/opencode-go/openai/v1/chat/completions' \
  -H 'Origin: https://odango-chan.github.io' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: content-type,x-opencode-session' \
  -H 'Access-Control-Request-Private-Network: true'
```

Expect HTTP 204, `Access-Control-Allow-Origin: https://odango-chan.github.io`,
`Access-Control-Allow-Private-Network: true`, and no
`Access-Control-Allow-Credentials`. Repeat with an unlisted Origin: expect 403
and **no** CORS grant. Actual requests still go through the existing token,
body-size and concurrency guards. Headers injected by a model upstream are
not allowed to override the Gateway CORS policy.

## Remote / public Pages safety

The static Pages site may be public even when the Gateway is private.
`server.cors_allowed_origins` only controls which browser origins may read
responses; it does **not** authorize users. An origin allowlist alone cannot
protect a paid shared Provider account against use by arbitrary visitors to
a public Pages deployment.

For non-loopback Gateway access, existing rules still require
`server.allow_remote: true` and a secret `server.access_token_env`. Protect
that endpoint with trusted network restrictions and HTTPS/TLS termination.
Do not preconfigure a shared `X-Gateway-Token` in the public Pages bundle,
a shipped settings file, browser-local hardcoded defaults, or CI artifacts.
If a publicly usable, multi-user hosted AI service is desired later, it needs
real server-side user identity, authorization, and rate/usage controls.
Those are **not** implemented in this lightweight Gateway.

## Release gates for Studio PR #208

- Keep the v1alpha5 Studio Authoring Core, Host-owned Change Set, Patch
  Engine, pinned Go/WASM Compiler and existing Gateway Client unchanged.
- Run the current opt-in synthetic-only provider smoke documented in
  `ca-skill-studio-demo/docs/testing/ai-authoring-live-gateway.md` with an
  actual reachable Gateway URL and model ID. Do not copy API keys to Studio.
- Separately qualify a *model-generated* Proposal -> selected atomic Patch ->
  exact Compiler PASS -> native WorkspaceEdit / Undo via the **Editor Tab**
  using a complete trusted synthetic Package.
- Retain preview-only behavior for unqualified 5GC Examples and Wait without
  trusted IngressCatalog. No live 5GC qualification is implied by CORS.

Only after those gates pass should Studio #208 become Ready and merge.
