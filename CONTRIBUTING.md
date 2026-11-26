# Contributing

Thanks for helping. Bug reports, fixes and small improvements are all welcome.

- **Bugs:** open an issue with the kdir version, OS, shell and the steps to
  reproduce it. For anything security-related, follow [SECURITY.md](SECURITY.md)
  instead.
- **Features:** open an issue to discuss it before writing a large change, so
  we can agree on the shape first.
- **Pull requests:** keep them focused, add or update tests, and make sure the
  checks below pass.

## Running the checks

```bash
go test -race ./...   # unit tests plus end-to-end tests in real zsh, bash and fish
golangci-lint run
```

The end-to-end tests skip any shell that isn't installed, so install zsh and
fish (and kubectl, for the cross-checks) to run the full suite. CI runs it on
Linux and macOS. See [docs/development.md](docs/development.md) for more.

## Ground rules

kdir must stay **fail-closed** and must **never write your kubeconfig**. A
change that weakens either needs a very good reason and a test that proves the
new behaviour.

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
