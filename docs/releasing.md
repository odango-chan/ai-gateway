# Releasing

The repository does not publish a version automatically from `main`. A release is created only when a maintainer pushes a version tag matching `v*`.

## Release process

1. Merge the implementation and verify the `main` branch is green.
2. Choose a semantic version such as `v0.2.0`.
3. Create and push the tag:

```sh
git checkout main
git pull --ff-only
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

4. The Release workflow runs tests and vet, then builds six CGO-free binaries:
   - Linux amd64 and arm64
   - Windows amd64 and arm64
   - macOS amd64 and arm64
5. Each platform archive contains the executable, MIT license, English and Chinese READMEs, both generic and Studio Pages example configurations under `configs/`, the complete `docs/` tree (including the architecture JSON, SVG preview, and standalone HTML), `SECURITY.md`, `CONTRIBUTING.md`, and the OpenCode Go smoke-test script plus the WSL/Linux and Windows Studio launchers under `scripts/`. It generates `SHA256SUMS` for all six archives and publishes a GitHub Release.

## v0.2.0-specific release acceptance

- Merge Gateway [#10](https://github.com/odango-chan/ai-gateway/pull/10)
  after tests and browser transport acceptance, and verify CI on the exact
  merged main commit.
- Confirm each of the six platform archives contains
  `configs/config.studio-pages.example.yaml`,
  `scripts/start-studio-pages.sh`,
  `scripts/start-studio-pages.ps1`, and `docs/browser-studio.md`.
- Perform a local/WSL browser test from the actual Pages origin using the
  loopback-only example; validate preflight, a real model response and no
  Provider API key in Pages artifacts. Do not confuse this with Studio #208's
  separate positive Editor Tab Apply acceptance.
- If the manual Release workflow is used, provide the exact merged main commit
  SHA and version `v0.2.0`. The workflow rejects a tag that already exists
  and ensures the release source is in main. Do not create a release directly
  from an unmerged feature branch.
- The default template contains only public URLs and placeholders. Provider
  key remains in the host process environment; no shared remote Gateway token
  is included in release assets.

## Before tagging

- Confirm the intended version and release notes.
- Run `go test ./...` and `go vet ./...`.
- Confirm the OpenCode Go endpoint examples match the current upstream documentation.
- Do not include real API keys or private configuration in release assets.
- If performing a live inference smoke test, select a model deliberately; provider usage or billing may apply.
