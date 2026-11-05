# cert-manager-webhook-contabo Helm chart

Installs the [cert-manager](https://cert-manager.io) DNS-01 webhook for
Contabo DNS. With it, cert-manager can get Let's Encrypt certificates,
including wildcards, for domains hosted at Contabo.

## Install

cert-manager must already be installed.

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

Then create a Secret with your Contabo API credentials and an issuer. The
chart prints both after installing; the full guide is
[docs/install.md](https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/install.md).

## Values

Every value is explained in [values.yaml](values.yaml) and in
[docs/configuration.md](https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/configuration.md#2-helm-values).
The most important ones:

| Value | Default | Meaning |
|---|---|---|
| `groupName` | `contabo.acme.codejourney.io` | Must match `webhook.groupName` in your issuers. |
| `certManager.namespace` | `cert-manager` | cert-manager's namespace; ClusterIssuer Secrets live here. |
| `certManager.serviceAccountName` | `cert-manager` | cert-manager's ServiceAccount. |
| `secretNames` | `[contabo-credentials]` | Secrets the webhook may read in cert-manager's namespace. |
| `extraSecretAccess` | `[]` | Secrets it may read in other namespaces (for namespaced Issuers). |
| `ambientCredentials.secretName` | `""` | Credentials for issuers that set no `secretName`. |
| `replicaCount` / `podDisruptionBudget.enabled` | `1` / `false` | High availability. |
| `logLevel` | `2` | `4` logs every Contabo API call. |

## Check the chart signature

```sh
cosign verify ghcr.io/hayone1/charts/cert-manager-webhook-contabo:0.1.0 \
  --certificate-identity-regexp '^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
