---
name: run-e2e
description: Run and debug the end-to-end test of the webhook in a real Kubernetes cluster (cert-manager + the Helm chart + an issued certificate), against the fake Contabo API with Pebble (default, no account) or the real Contabo API with Let's Encrypt staging. Use when asked to "run e2e", test in a cluster, or check a change before a release.
---

# Run the end-to-end test

`test/e2e/run.sh` installs cert-manager and this chart, creates issuers and
certificates, and checks that every challenge record is removed afterwards.

| Mode | Talks to | Needs | Used by |
|---|---|---|---|
| `E2E_MODE=fake` (default) | fake API + fake DNS and Pebble in the cluster | a throwaway cluster (kind) | CI, every PR |
| `E2E_MODE=real` | api.contabo.com, ns1/2/3.contabo.net, Let's Encrypt staging | `.env` with credentials and `E2E_ZONE` | maintainers, before a release |

**Never put Contabo credentials into GitHub.**

## Fake mode

```sh
make e2e-images
kind create cluster
kind load docker-image cert-manager-webhook-contabo:e2e fake-contabo:e2e
make e2e
```

It changes cert-manager's DNS flags (`--dns01-recursive-nameservers-only`
pointing at the fake), so never run it against a cluster you care about.
It checks: apex + wildcard certificate, a certificate in a child zone, a
namespaced Issuer with its own Secret, cleanup through 25 failing deletes,
and that an unrelated TXT record on the same name survives.

## Real mode

1. Ask the user which zone and cluster to use if `E2E_ZONE` is not set (never guess).
2. Put the image into the cluster. k3s:
   `docker save IMG | ssh root@HOST k3s ctr -n k8s.io images import -`.
   Over a slow link, `scp` the tar and import it on the host instead.
3. `set -a; . ./.env; set +a` (do not print the values), then
   `E2E_MODE=real make e2e`. Add `E2E_INSTALL_CERT_MANAGER=true` if
   cert-manager is not installed; tell the user first, it adds cluster-wide CRDs.

Each run uses its own `groupName` (`e2e-<id>.contabo.acme.codejourney.io`),
Helm release, ClusterIssuer and names under `cm-e2e-<id>.<zone>`, and
removes them on exit (`E2E_KEEP=true` keeps them).

## Reading a failure

The script prints challenges, pods and the last logs of the webhook,
cert-manager, the fake and Pebble.

| Symptom | Likely cause |
|---|---|
| APIService never `Available` | cainjector not running yet, or the webhook pod crashes (check its log). |
| `Waiting for DNS-01 challenge propagation` for 1-3 minutes (real mode) | Normal: Contabo's name servers cache "not found" for about 60 s. |
| Challenge error `public DNS says ...` | The zone's delegation does not match the Contabo account. |
| `the delete outage was never hit` | The fault count is too low for the number of parallel cleanups. |
| kind node container stopped | Not enough memory for Docker; stop other containers and retry. |
