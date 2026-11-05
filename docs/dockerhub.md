# cert-manager-webhook-contabo

A [cert-manager](https://cert-manager.io) DNS-01 webhook for **Contabo DNS**.
It lets cert-manager get free Let's Encrypt certificates, including
wildcards (`*.example.com`), for domains whose DNS is hosted at Contabo.

- Source, docs and issues: https://github.com/hayone1/cert-manager-webhook-contabo
- Install guide: https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/install.md

## Install with Helm

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager \
  --set image.repository=docker.io/hayone/cert-manager-webhook-contabo
```

Then create a Secret with your Contabo API credentials and a ClusterIssuer
with this solver:

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

Tags have no `v`: `0.1.0`, `0.1`, `latest`. The same image (same digest) is
on `ghcr.io/hayone1/cert-manager-webhook-contabo`.

## Security

- Built by GitHub Actions only, for `linux/amd64` and `linux/arm64`.
- Signed with cosign (keyless), with SBOM and build provenance.
- Distroless base: no shell, no package manager, runs as user 65532,
  works with a read-only file system.

Check the signature (cosign 3 or newer):

```sh
cosign verify docker.io/hayone/cert-manager-webhook-contabo:0.1.0 \
  --certificate-identity-regexp '^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## License

Apache-2.0. Not made or supported by Contabo.
