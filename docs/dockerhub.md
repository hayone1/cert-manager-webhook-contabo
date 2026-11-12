# cert-manager-webhook-contabo

A [cert-manager](https://cert-manager.io) webhook for **Contabo DNS**.
With this webhook, cert-manager can get free Let's Encrypt certificates
for domains whose DNS records are kept at Contabo. This includes wildcard
certificates (`*.example.com`).

The webhook adds and removes the TXT record `_acme-challenge.<domain>`
through the Contabo API. Let's Encrypt checks this record to see that you
control the domain (the DNS-01 challenge).

- Source code, docs and issues: https://github.com/hayone1/cert-manager-webhook-contabo
- Install guide: https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/install.md

## Install with Helm

This command installs the webhook with the image from Docker Hub:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager \
  --set image.repository=docker.io/hayone/cert-manager-webhook-contabo
```

Then create a Secret with your Contabo API credentials, and a
ClusterIssuer with this solver:

```yaml
solvers:
  - dns01:
      webhook:
        groupName: contabo.acme.codejourney.io
        solverName: contabo
        config:
          secretName: contabo-credentials
```

## Tags

| Tag | Example | Changes later? |
|---|---|---|
| exact version | `0.1.0` and `v0.1.0` | never |
| minor version | `0.1` | yes, to the newest patch |
| `latest` | | yes, to the newest release |

The same image, with the same digest, is also on
`ghcr.io/hayone1/cert-manager-webhook-contabo`.

## Security

- GitHub Actions builds every image, for `linux/amd64` and `linux/arm64`.
- Every image is signed with cosign (keyless) and has an SBOM and build
  provenance.
- The image has no shell and no package manager. It runs as user 65532 and
  works with a read-only file system.

Check the signature with cosign 3 or newer:

```sh
cosign verify docker.io/hayone/cert-manager-webhook-contabo:0.1.2 \
  --certificate-identity-regexp '^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## License

Apache-2.0. Contabo does not make or support this project.
