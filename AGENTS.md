# Notes for AI coding agents

This file tells AI agents (Claude Code, Codex, Copilot, Cursor, ...) how to
work in this repository. Humans may find it useful too.

## What this project is

A [cert-manager](https://cert-manager.io) DNS-01 webhook for Contabo DNS.
It is a Kubernetes aggregated API server: cert-manager calls it through the
Kubernetes API (`/apis/<groupName>/v1alpha1/contabo`) to add (`Present`)
and remove (`CleanUp`) `_acme-challenge` TXT records through the Contabo API.
Default `groupName`: `contabo.acme.codejourney.io`. `solverName`: `contabo`.

## Layout

| Path | What is there |
|---|---|
| `cmd/webhook` | `main`, `GROUP_NAME` check, slog-to-logr bridge |
| `internal/solver` | The `webhook.Solver`: config parsing, Secret loading, client cache, zone choice, Present, CleanUp |
| `internal/contabo` | Contabo client (copied from external-dns-contabo-webhook; keep in sync by hand) |
| `internal/contabo/contabotest` | Fake Contabo API with the real API's quirks, fault injection, and a DNS server |
| `test/conformance` | cert-manager conformance suite (build tag `conformance`, needs envtest) |
| `test/e2e` | `run.sh`: kind + cert-manager + Pebble + fake (CI), or real Contabo + Let's Encrypt staging (local) |
| `charts/cert-manager-webhook-contabo` | Helm chart |
| `docs/` | User and maintainer docs; `edge-cases.md` lists every known edge case |

## Commands

```sh
make test          # unit tests, -race (no network, no account)
make lint          # golangci-lint v2 (also lints the conformance build tag)
make conformance   # cert-manager conformance suite against the fake (Linux/macOS)
make chart-lint    # helm lint + kubeconform
make e2e-images && make e2e      # needs a throwaway cluster (kind)
E2E_MODE=real make e2e           # real Contabo + LE staging; local only
make conformance-real            # real Contabo; local only
```

Run `make test` and `make lint` before you say a change is done. Run
`make conformance` when you change the solver or the fake.

## Rules

1. **Never print, log or commit secrets.** `.env`, `.mcp.json`, kubeconfigs
   and anything under `.notes/` or `_debug/` are private. Do not read `.env`
   values into the conversation; pass them to commands through the
   environment instead.
2. **Never create or delete Contabo zones** from code.
3. **CI never uses a real Contabo account.** GitHub workflows test against
   the fake only. Do not add Contabo secrets to the repo or to GitHub.
4. **Present only adds. CleanUp only deletes TXT records whose value equals
   the challenge key.** Never replace or remove other values on the same name.
5. **Idempotent.** Present of an existing record, a 409 on create, and
   CleanUp of a missing record are all success.
6. **Secrets come only from the challenge's `ResourceNamespace`.** Never add
   an issuer setting that reads another namespace or changes the API URL.
7. **Every call stays under the API server's 60 s request timeout**
   (`OperationTimeout`, default 45 s).
8. **The fake API is the contract.** Real behavior is written down in
   `docs/contabo-api.md` and copied in `internal/contabo/contabotest`. If you
   learn something new, update both, plus a test, in this repo and in
   external-dns-contabo-webhook.
9. **New edge case?** Handle it, test it, and add it to `docs/edge-cases.md`.
   If it cannot be handled, document it there under "Cannot be solved".
10. **Plain language in docs.** Write for a 9th grader: short sentences,
    explain terms, give copy-paste examples.
11. **Do not mention specific domain registrars** in docs or code. Say "your
    domain registrar".
12. **Pin GitHub Actions by full commit SHA** with the version in a comment.
    Every job sets its own minimal `permissions`. Keep the job names
    `CI passed` and `kind + cert-manager + Pebble + fake Contabo` stable;
    they are required checks.

## Contabo API facts that matter for DNS-01

- Names are stored exactly as sent. `_acme-challenge`, `_acme-challenge.example.com`
  and `_acme-challenge.example.com.` are the same name. The webhook always
  writes the **full** name (a relative name that ends in the zone name would
  be taken as full).
- TXT data is stored as sent; `abc` and `"abc"` are served the same and are
  duplicates (409). `txtValue` strips quotes before comparing with the key.
- Duplicate create (any name spelling, same data) is HTTP 409: success.
- Delete of a missing record ID is 204. Deletes failed with 500 for about
  90 s on 2026-11-03: retry, then fail so cert-manager retries.
- A change reaches ns1/ns2/ns3 in under a second, but each name server
  caches "not found" for about 60 s and answers for about 20 s.
- Tokens live 300 s; the client re-runs the password grant.
- Rate limit: about 15 requests per second. The client defaults to 10.

## Skills

Step-by-step guides live in `.claude/skills/`:

- `run-e2e` - run and debug the end-to-end test (fake or real)
- `run-conformance` - run cert-manager's conformance suite
- `probe-contabo-api` - safely check how the real Contabo API behaves
- `release` - cut a release and check the published image and chart
