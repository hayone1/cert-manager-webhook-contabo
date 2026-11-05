# Development

## What you need

- [Go](https://go.dev/dl/) (the version in `go.mod`)
- `make`
- Docker, to build the image
- For the conformance suite: Linux or macOS (envtest downloads `etcd` and
  `kube-apiserver` binaries)
- For the end-to-end test: [kind](https://kind.sigs.k8s.io), `kubectl`,
  `helm`, `jq`, `curl`

None of the normal tests need a Contabo account.

## Layout

| Path | What is there |
|---|---|
| `cmd/webhook` | `main`: reads `GROUP_NAME` and settings, starts cert-manager's webhook server, log bridge |
| `internal/solver` | The cert-manager `Solver`: config, Secrets, client cache, zone choice, Present, CleanUp |
| `internal/contabo` | Small Contabo DNS client: OAuth2 password grant, retries, rate limit, pagination (shared with external-dns-contabo-webhook) |
| `internal/contabo/contabotest` | In-memory fake Contabo API plus a DNS server that serves its records |
| `test/conformance` | cert-manager's official DNS-01 conformance suite (build tag `conformance`) |
| `test/e2e` | End-to-end test in a real cluster (`run.sh`); `fakecontabo/` is the fake API image |
| `charts/cert-manager-webhook-contabo` | The Helm chart |
| `deploy/examples` | Example Secret, issuers and certificate |

## The everyday loop

```sh
make test     # unit tests with the race detector
make lint     # golangci-lint
make fmt      # format the code
make build    # build ./bin/webhook
```

## Test levels

| Level | Command | Talks to | Needs | Runs in CI |
|---|---|---|---|---|
| Unit | `make test` | fake API (in memory) | nothing | yes |
| Conformance | `make conformance` | fake API + fake DNS, real kube-apiserver (envtest) | Linux/macOS | yes |
| Chart | `make chart-lint` | nothing | helm | yes (plus chart-testing) |
| End-to-end, fake | `make e2e-images` then `make e2e` | real cert-manager, Pebble, fake API + DNS in a kind cluster | kind | yes, on every PR |
| Conformance, real | `make conformance-real` | real Contabo API and name servers | `.env` | **never** |
| End-to-end, real | `E2E_MODE=real make e2e` | real Contabo, real DNS, Let's Encrypt staging | `.env`, a cluster with cert-manager | **never** |

CI never uses a real Contabo account. Do not add Contabo secrets to GitHub.

## Conformance suite

cert-manager ships a test suite that every DNS-01 webhook should pass. It
starts a real `kube-apiserver` and `etcd` (envtest), calls Present and
CleanUp, and checks the result with real DNS queries.

```sh
make conformance
```

The first run downloads the envtest binaries into `./bin` (set
`LOCALBIN=...` to put them elsewhere). The suite runs against the fake API,
and the fake's DNS server answers the DNS queries.

## End-to-end test (fake mode)

Runs everything a user runs: cert-manager, this chart, a ClusterIssuer and
a namespaced Issuer, and a certificate for `example.com` + `*.example.com`,
issued by **Pebble** (Let's Encrypt's test server). The fake Contabo API
also serves DNS, and both cert-manager and Pebble use it. During the test,
25 record deletes fail with HTTP 500 to prove that cleanup survives an outage.

```sh
make e2e-images
kind create cluster
kind load docker-image cert-manager-webhook-contabo:e2e fake-contabo:e2e
make e2e
```

Use a throwaway cluster: fake mode installs cert-manager with DNS settings
that point at the fake.

## Tests against the real Contabo API

Only on your own machine. Copy `.env.example` to `.env` and fill it in.
Tests only use names under `cm-conf-<id>.<zone>` and `cm-e2e-<id>.<zone>`
and remove them afterwards.

```sh
make conformance-real          # conformance suite, real API and name servers
E2E_MODE=real make e2e         # needs cert-manager in the cluster
E2E_MODE=real E2E_INSTALL_CERT_MANAGER=true make e2e   # installs cert-manager first
```

In real mode the end-to-end test uses Let's Encrypt **staging**, and
cert-manager's normal DNS checks against `ns1/ns2/ns3.contabo.net`. The
webhook image must be in the cluster, for example for k3s:

```sh
docker save cert-manager-webhook-contabo:e2e | ssh root@HOST k3s ctr -n k8s.io images import -
```

Last real runs (k3s v1.36, cert-manager v1.21.3, zone `codetrekker.io`):

| Date | Test | Result |
|---|---|---|
| 2026-11-04 | `make conformance-real` | **Passed** in 156 s: Present, propagation to ns1/ns2/ns3, two values on one name, CleanUp of one value only. |
| 2026-11-04 | `E2E_MODE=real make e2e` | Webhook part passed, but **issuance failed** on Let's Encrypt's side: `DNS problem: query timed out looking up TXT`. Let's Encrypt could not reach Contabo's name servers that day. See [edge cases](edge-cases.md#lets-encrypt-cannot-reach-contabos-name-servers). |
| 2026-11-05 | `E2E_MODE=real make e2e` | **Passed** in 247 s. Let's Encrypt staging issued one certificate for the name and its wildcard after 218 s. Every challenge record was removed, the unrelated record on the same name was kept, and the name servers stopped serving the challenge 21 s after cleanup. |

If a real run shows a difference from the fake: update
`internal/contabo/contabotest`, [contabo-api.md](contabo-api.md) and a test.

## Rules for changes

1. Add a test. A bug fix needs a test that fails without the fix.
2. Never log secrets, tokens or `Authorization` headers.
3. Present must only add; CleanUp must only delete records whose value is
   the challenge key.
4. Never create or delete zones.
5. Write docs for a 9th grader: short sentences, explain terms, give
   copy-paste examples.
