# Releasing

The repository does not publish a version automatically from `main`. A release is created only when a maintainer pushes a version tag matching `v*`.

## Release process

1. Merge the implementation and verify the `main` branch is green.
2. Choose a semantic version such as `v0.1.0`.
3. Create and push the tag:

```sh
git checkout main
git pull --ff-only
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

4. The Release workflow runs tests and vet, then builds six CGO-free binaries:
   - Linux amd64 and arm64
   - Windows amd64 and arm64
   - macOS amd64 and arm64
5. The workflow packages each executable with the MIT license, English and Chinese READMEs, and example configuration. It generates `SHA256SUMS` and publishes a GitHub Release.

## Before tagging

- Confirm the intended version and release notes.
- Run `go test ./...` and `go vet ./...`.
- Confirm the OpenCode Go endpoint examples match the current upstream documentation.
- Do not include real API keys or private configuration in release assets.
- If performing a live inference smoke test, select a model deliberately; provider usage or billing may apply.
