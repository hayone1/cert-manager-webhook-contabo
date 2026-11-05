---
name: run-conformance
description: Run cert-manager's official DNS-01 webhook conformance suite against the solver, with the fake Contabo API (default) or the real API. Use after changing internal/solver or the fake, or when asked to "run conformance".
---

# Run the conformance suite

The suite is `github.com/cert-manager/cert-manager/test/acme`. It starts a
real kube-apiserver and etcd (envtest), applies the Secret from
`test/conformance/testdata/fake`, calls Present and CleanUp, and checks the
TXT records with DNS queries. The fake Contabo API serves those queries
through its own DNS server (`contabotest.StartDNS`).

## Steps

1. Linux or macOS only (envtest binaries). On Windows use WSL.
2. `make conformance` (downloads `setup-envtest` and the binaries into
   `./bin`; set `LOCALBIN=$HOME/.cache/cmwc-bin` to keep them elsewhere).
3. Real API (local only, never CI): fill `.env` (see `.env.example`), then
   `make conformance-real`. It writes only under `cm-conf-<id>.<zone>` and
   checks Contabo's name servers directly. It can take several minutes
   because the name servers cache "not found" answers for about 60 s.

## Notes

- The file has the build tag `conformance`: cert-manager's test package
  panics at start if the envtest binaries are missing, so `go test ./...`
  must not compile it.
- `RunConformance` nests two control planes; the test calls `RunBasic` and
  `RunExtended` instead, like cert-manager's webhook-example.
- `Initialize` is called once per control plane, so it must be safe to call twice.
