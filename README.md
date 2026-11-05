# cert-manager-webhook-contabo

[![CI](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/ci.yml/badge.svg)](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/ci.yml)
[![End-to-end](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/e2e.yml/badge.svg)](https://github.com/hayone1/cert-manager-webhook-contabo/actions/workflows/e2e.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/hayone1/cert-manager-webhook-contabo/badge)](https://scorecard.dev/viewer/?uri=github.com/hayone1/cert-manager-webhook-contabo)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

Get free [Let's Encrypt](https://letsencrypt.org) certificates, **including
wildcard certificates** (`*.example.com`), for domains whose DNS is hosted
at [Contabo](https://contabo.com).

This is a plug-in ("webhook") for [cert-manager](https://cert-manager.io),
the tool that gets and renews certificates in Kubernetes.

## What it does, in plain words

To give you a certificate, Let's Encrypt asks you to prove that you control
the domain. One way to prove it is the **DNS-01 challenge**: you put a
secret text value into a DNS record called `_acme-challenge.<your domain>`,
and Let's Encrypt looks it up. Wildcard certificates can only be proved this way.

cert-manager does all of this for you, but it needs help to talk to your DNS
provider. This webhook is that help for Contabo:

1. cert-manager asks the webhook to add the TXT record. The webhook adds it
   through the Contabo API.
2. cert-manager waits until Contabo's name servers show the record, then
   Let's Encrypt checks it.
3. cert-manager asks the webhook to remove the record. The webhook removes
   that one record and nothing else.

## Features

- Wildcard and normal certificates, and both in one certificate.
- Works with `ClusterIssuer` and namespaced `Issuer`, each with its own Contabo account if you like.
- Only ever touches the one TXT record it created. Your other records are safe.
- Handles Contabo's quirks: every name spelling, quoted TXT values, duplicate
  records, short outages, rate limits.
- Finds the right zone on its own, also for sub-zones, and explains clearly
  when your domain does not point to Contabo.
- Works with CNAME delegation (`cnameStrategy: Follow`), so a domain hosted
  elsewhere can use a Contabo zone for its challenges.
- Small and locked down: no shell in the image, non-root, read-only file
  system, reads only the Secrets you allow.
- Signed multi-arch images (amd64, arm64) and a signed Helm chart.
- Tested on every change with cert-manager's official conformance suite and
  a full certificate issue in a real cluster, using a fake Contabo API, so
  no real account is needed.

## Quick start

You need a Kubernetes cluster with [cert-manager](https://cert-manager.io/docs/installation/) installed (tested with v1.21),
a domain whose name servers are `ns1.contabo.net`, `ns2.contabo.net` and
`ns3.contabo.net`, and your Contabo API credentials from
[my.contabo.com/api/details](https://my.contabo.com/api/details).

**1. Install the webhook** into cert-manager's namespace:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

**2. Save your Contabo API credentials** in a Secret:

```sh
kubectl -n cert-manager create secret generic contabo-credentials \
  --from-literal=client-id='INT-12345678' \
  --from-literal=client-secret='your-client-secret' \
  --from-literal=api-user='you@example.com' \
  --from-literal=api-password='your-api-password'
```

**3. Create a ClusterIssuer.** Start with Let's Encrypt *staging*: it has
high limits and is made for testing. Switch to production when it works.

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

**4. Ask for a certificate:**

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

After a minute or two, `READY` turns `True` and the certificate is in the
Secret `example-com-tls`.

The full guide, with namespaced Issuers and production settings, is in
[docs/install.md](docs/install.md).

## Documentation

| Guide | What is in it |
|---|---|
| [Install](docs/install.md) | Step-by-step setup, staging and production, Issuer vs ClusterIssuer |
| [Configuration](docs/configuration.md) | Every issuer setting, Helm value and environment variable |
| [How it works](docs/how-it-works.md) | What happens during a challenge, and why |
| [Edge cases](docs/edge-cases.md) | Every tricky situation we know of, how it is handled, and the ones that cannot be solved |
| [Troubleshooting](docs/troubleshooting.md) | Error messages and what to do about them |
| [Contabo API notes](docs/contabo-api.md) | What we learned about the real Contabo API |
| [Development](docs/development.md) | Build, test, conformance suite, end-to-end tests |
| [Releasing](docs/releasing.md) | How a release is made and how to check it |

## Images and chart

| What | Where |
|---|---|
| Image (GitHub) | `ghcr.io/hayone1/cert-manager-webhook-contabo` |
| Image (Docker Hub) | `docker.io/hayone/cert-manager-webhook-contabo` |
| Helm chart | `oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo` |

Image tags have no `v`: use `0.1.0`, not `v0.1.0`. Images and the chart are
signed; see [docs/releasing.md](docs/releasing.md#check-a-release) to check them.

## Related projects

- [external-dns-contabo-webhook](https://github.com/hayone1/external-dns-contabo-webhook):
  keeps your Contabo DNS records in sync with your Kubernetes Services and
  Ingresses. It uses the same Secret format, so one Secret can serve both.

## Contributing

Bug reports, docs fixes and code are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).
Security problems: see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). If you copy or change this code, keep the
[NOTICE](NOTICE) file, as the license asks.

This project is not made or supported by Contabo.
