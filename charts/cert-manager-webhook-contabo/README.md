# cert-manager-webhook-contabo Helm chart

This chart installs a [cert-manager](https://cert-manager.io) webhook for
Contabo DNS. With this webhook, cert-manager can get Let's Encrypt
certificates for domains whose DNS records are kept at Contabo. This
includes wildcard certificates.

## Install

cert-manager must be installed first.

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

Next, create a Secret with your Contabo API credentials, and an issuer.
After the install, the chart prints an example of both. The
[install guide](https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/install.md)
explains every step.

## Values

[values.yaml](values.yaml) and the
[configuration reference](https://github.com/hayone1/cert-manager-webhook-contabo/blob/main/docs/configuration.md#2-helm-values)
explain every value. These are the most important values:

| Value | Default | Meaning |
|---|---|---|
| `groupName` | `contabo.acme.codejourney.io` | Must be the same as `webhook.groupName` in your issuers. |
| `certManager.namespace` | `cert-manager` | The namespace of cert-manager. The Secrets of ClusterIssuers must be in this namespace. |
| `certManager.serviceAccountName` | `cert-manager` | The ServiceAccount of cert-manager. Only this ServiceAccount may call the webhook. |
| `secretNames` | `[contabo-credentials]` | The Secrets that the webhook may read in the namespace of cert-manager. |
| `extraSecretAccess` | `[]` | The Secrets that the webhook may read in other namespaces, for namespaced Issuers. |
| `ambientCredentials.secretName` | `""` | A Secret with credentials for issuers that name no `secretName`. |
| `replicaCount` / `podDisruptionBudget.enabled` | `1` / `false` | Run more than one webhook pod, and keep one running during node drains. |
| `logLevel` | `2` | `2` logs one line per record. `4` logs every Contabo API call. |

## Check the chart signature

```sh
cosign verify ghcr.io/hayone1/charts/cert-manager-webhook-contabo:0.1.2 \
  --certificate-identity-regexp '^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
