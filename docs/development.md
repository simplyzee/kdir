# Development

```bash
go test -race ./...        # unit tests, plus end-to-end tests in real zsh, bash and fish
golangci-lint run
scripts/bench.sh           # hyperfine benchmarks
```

The end-to-end tests in `testdata/script` (zsh and bash) and
`testdata/script-fish` use [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript).
They drive real shells, including interactive ones, and cross-check every
result against the real `kubectl` when it's installed. Missing shells are
skipped.

## Releasing

Push a tag such as `v0.1.0`. The release workflow runs the tests, then
GoReleaser builds linux/darwin × amd64/arm64 archives and a GitHub release.

The Homebrew cask is generated on every release but only published when the
`HOMEBREW_TAP_GITHUB_TOKEN` secret is set. To enable it:

1. Create the public repository `thomaschaplin/homebrew-tap`.
2. Create a fine-grained token with *Contents: read and write* on that
   repository, and add it to this repository as the
   `HOMEBREW_TAP_GITHUB_TOKEN` secret.

The binaries aren't notarised, so the cask clears macOS's quarantine flag
after install.

