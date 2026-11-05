# cert-manager-webhook-contabo

[![CI](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/ci.yml/badge.svg)](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/ci.yml)
[![End-to-end](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/e2e.yml/badge.svg)](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/e2e.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hayone1/cert-manager-webhook-contabo/badge)](https://scorecard.dev/viewer/?uri=github.com/hayone1/cert-manager-webhook-contabo)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cert-manager-webhook-contabo)](https://artifacthub.io/packages/search?repo=cert-manager-webhook-contabo)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

Get free [Let's Encrypt](https://letsencrypt.org) certificates for domains
whose DNS records are kept at [Contabo](https://contabo.com). This includes
**wildcard certificates** (`*.example.com`).

This project is a plug-in for [cert-manager](https://cert-manager.io).
cert-manager is a tool that gets and renews certificates in Kubernetes.
cert-manager calls this kind of plug-in a **webhook**.

## What the webhook does

Before Let's Encrypt gives you a certificate, you must prove that you
control the domain. One way to prove it is the **DNS-01 challenge**:

1. Let's Encrypt gives cert-manager a secret text, the **challenge key**.
2. The challenge key must appear in a TXT record named
   `_acme-challenge.<your domain>`. This guide calls that record the
   **challenge record**.
3. Let's Encrypt looks up the challenge record. If it finds the challenge
   key, you get the certificate.

Wildcard certificates can only be proved with the DNS-01 challenge.

cert-manager does these steps for you, but it cannot change DNS records at
Contabo by itself. The webhook does that part:

1. cert-manager asks the webhook to add the challenge record. The webhook
   adds it through the Contabo API.
2. cert-manager waits until Contabo's name servers (`ns1.contabo.net`,
   `ns2.contabo.net` and `ns3.contabo.net`) show the challenge record.
   Then Let's Encrypt checks it.
3. cert-manager asks the webhook to remove the challenge record. The
   webhook removes only that one record.

## Features

- Gets wildcard certificates, normal certificates, and certificates with
  both kinds of names.
- Works with a `ClusterIssuer` (one issuer for the whole cluster) and with
  an `Issuer` (one issuer for one namespace). Each issuer can use its own
  Contabo account.
- Never changes or deletes your other DNS records. The webhook only
  deletes a TXT record whose value is the challenge key.
- Handles the ways the Contabo API differs from other DNS APIs, for example
  how it stores record names and quoted TXT values. The
  [edge cases](docs/edge-cases.md) page lists every case.
- Retries when the Contabo API has a short outage or a rate limit.
- Finds the right Contabo zone by itself, also for sub-zones. If your
  domain does not use Contabo's name servers, the error message says so.
- Works with CNAME delegation (`cnameStrategy: Follow`). A domain whose DNS
  is not at Contabo can still use a Contabo zone for its challenges.
- Runs with few rights: no shell in the image, not as root, on a read-only
  file system. It can only read the Kubernetes Secrets that you allow.
- Comes as signed images for amd64 and arm64 and as a signed Helm chart.
- Every change is tested with cert-manager's official conformance tests and
  with a full certificate issue in a test cluster. These tests use a fake
  Contabo API, so they need no real Contabo account.

## Quick start

You need:

- a Kubernetes cluster with
  [cert-manager](https://cert-manager.io/docs/installation/) installed
  (tested with cert-manager v1.21),
- a domain whose name servers are `ns1.contabo.net`, `ns2.contabo.net` and
  `ns3.contabo.net`. You set the name servers at your domain registrar,
- your Contabo API credentials from
  [my.contabo.com/api/details](https://my.contabo.com/api/details): client
  ID, client secret, API user and API password.

**Step 1. Install the webhook** into cert-manager's namespace:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

**Step 2. Save your Contabo API credentials** in a Kubernetes Secret named
`contabo-credentials`:

```sh
kubectl -n cert-manager create secret generic contabo-credentials \
  --from-literal=client-id='INT-12345678' \
  --from-literal=client-secret='your-client-secret' \
  --from-literal=api-user='you@example.com' \
  --from-literal=api-password='your-api-password'
```

**Step 3. Create a ClusterIssuer.** Start with Let's Encrypt **staging**.
Staging is made for tests and has high limits. Its certificates are not
trusted by browsers. Switch to production when staging works.

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: you@example.com # Let's Encrypt writes here before a certificate expires
    privateKeySecretRef:
      name: letsencrypt-staging-account
    solvers:
      - dns01:
          webhook:
            groupName: contabo.acme.codejourney.io
            solverName: contabo
            config:
              secretName: contabo-credentials
```

**Step 4. Ask for a certificate:**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  dnsNames:
    - example.com
    - "*.example.com"
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-staging
```

```sh
kubectl get certificate example-com --watch
```

After 2 to 5 minutes, `READY` changes to `True`. The certificate is then in
the Secret `example-com-tls`.

The [install guide](docs/install.md) has more: namespaced Issuers,
production settings and checks.

## Documentation

| Guide | What is in it |
|---|---|
| [Install](docs/install.md) | Setup step by step, staging and production, Issuer and ClusterIssuer |
| [Configuration](docs/configuration.md) | Every issuer setting, Helm value and environment variable |
| [How it works](docs/how-it-works.md) | What happens during a challenge, and why |
| [Edge cases](docs/edge-cases.md) | Every unusual case we know of, how the webhook handles it, and the cases it cannot solve |
| [Troubleshooting](docs/troubleshooting.md) | Error messages and what to do about them |
| [Contabo API notes](docs/contabo-api.md) | How the real Contabo API behaves |
| [Development](docs/development.md) | Build, tests, conformance tests, end-to-end tests |
| [Releasing](docs/releasing.md) | How a release is made and how to check it |

## Images and chart

| What | Where |
|---|---|
| Image on GitHub | `ghcr.io/hayone1/cert-manager-webhook-contabo` |
| Image on Docker Hub | `docker.io/hayone/cert-manager-webhook-contabo` |
| Helm chart | `oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo` |
| Artifact Hub page | [artifacthub.io](https://artifacthub.io/packages/helm/cert-manager-webhook-contabo/cert-manager-webhook-contabo) |

Each version has an image tag with and without `v`, for example `0.1.0` and
`v0.1.0`. The chart version has no `v`: `--version 0.1.0`. The images and
the chart are signed. [Check a release](docs/releasing.md#check-a-release)
shows how to verify the signatures.

## Related projects

- [external-dns-contabo-webhook](https://github.com/hayone1/external-dns-contabo-webhook)
  keeps your Contabo DNS records in sync with your Kubernetes Services and
  Ingresses. It uses the same Secret format, so one Secret can serve both
  projects.

## Contributing

Bug reports, fixes to the docs and code are welcome. Read
[CONTRIBUTING.md](CONTRIBUTING.md) first. To report a security problem,
read [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). If you copy or change this code, keep the
[NOTICE](NOTICE) file. The license asks for that.

Contabo does not make or support this project.
