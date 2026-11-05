# Contributing

Thanks for helping! Bug reports, docs fixes and code are all welcome.

## Before you start

- For a small fix (typo, clear bug), open a pull request directly.
- For a bigger change (new setting, new behavior), open an issue first so we
  can agree on the idea before you spend time on it.
- Be kind. See the [code of conduct](CODE_OF_CONDUCT.md).
- Found a security problem? Do **not** open an issue. Follow [SECURITY.md](SECURITY.md).

## What you need

- [Go](https://go.dev/dl/) (the version in `go.mod`)
- `make`
- Docker, if you want to build the image
- For the conformance suite: Linux or macOS
- For the end-to-end test: [kind](https://kind.sigs.k8s.io), `kubectl`, `helm`, `jq` and `curl`

No Contabo account is needed for any of the normal tests.

## The everyday loop

```sh
git clone https://github.com/hayone1/cert-manager-webhook-contabo.git
cd cert-manager-webhook-contabo

make test          # unit tests, with the race detector
make lint          # golangci-lint
make conformance   # cert-manager's DNS-01 conformance suite (downloads envtest)
make fmt           # format the code
```

Everything runs against a fake Contabo API (`internal/contabo/contabotest`)
that copies how the real Contabo API behaves, plus a small DNS server that serves the
fake's records. See [docs/development.md](docs/development.md) for all test
levels, including the end-to-end test in kind.

## Rules for changes

1. **Add a test.** A bug fix needs a test that fails without the fix.
2. **Never touch other records.** Present only adds; CleanUp only deletes
   the TXT record whose value is the challenge key.
3. **Learned something new about the Contabo API?** Update the fake server
   in `internal/contabo/contabotest` and [docs/contabo-api.md](docs/contabo-api.md).
4. **Found a new edge case?** Add it to [docs/edge-cases.md](docs/edge-cases.md).
5. **Never log secrets.** No credentials, tokens or `Authorization` headers
   in logs, errors or test output.
6. **Write docs in plain words.** Short sentences. Explain any term a new
   user might not know.

## Pull requests

- Use a short title in the [Conventional Commits](https://www.conventionalcommits.org/)
  style, for example `fix: keep TXT quotes when comparing keys`.
- Fill in the pull request template.
- CI must pass: lint, tests, conformance suite, vulnerability check, chart
  checks, image build and scan, workflow checks, and the end-to-end test.
- A maintainer reviews and merges it.

## Using AI coding agents

Agents such as Claude Code, Codex or Copilot are fine to use. Read
[AGENTS.md](AGENTS.md): it lists the rules agents must follow here, and the
`.claude/skills/` folder holds step-by-step guides for common tasks. You are
still responsible for every line you submit, so read and test it.

## License

By sending a contribution, you agree that it is licensed under the
[Apache License 2.0](LICENSE), like the rest of the project.
