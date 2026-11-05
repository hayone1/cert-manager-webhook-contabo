# Configuration reference

There are three places to configure the webhook:

1. the **issuer** (`ClusterIssuer` or `Issuer`): which Contabo account and how
2. the **Helm values**: how the webhook runs in your cluster
3. **environment variables** of the webhook pod (the Helm chart sets them for you)

## 1. Issuer settings

The solver block in your issuer:

```yaml
solvers:
  - dns01:
      webhook:
        groupName: contabo.acme.codejourney.io # must match the Helm value groupName
        solverName: contabo                   # always "contabo"
        config:
          secretName: contabo-credentials
          ttl: 60
```

Everything under `config` is read by this webhook:

| Setting | Required | Default | What it does |
|---|---|---|---|
| `secretName` | yes, unless you use [ambient credentials](#ambient-credentials) | | Name of the Secret with your Contabo API credentials. For a `ClusterIssuer` it is read from cert-manager's namespace. For an `Issuer` it is read from the Issuer's own namespace. |
| `secretKeys.clientID` | no | `client-id` | Key in the Secret that holds the Client ID. |
| `secretKeys.clientSecret` | no | `client-secret` | Key that holds the Client Secret. |
| `secretKeys.apiUser` | no | `api-user` | Key that holds the API user (your e-mail). |
| `secretKeys.apiPassword` | no | `api-password` | Key that holds the API password. |
| `ttl` | no | `60` | Time to live of the TXT record, in seconds, from 1 to 86400. Short is better: the record only lives for a few minutes. `"60"` (a string) works too. |
| `zone` | no | found automatically | The Contabo zone to write to, for example `example.com`. Leave it out. Set it only if the webhook picks the wrong zone and you understand why (see [edge cases](edge-cases.md#the-zone-in-dns-and-the-zone-at-contabo-differ)). |

The default key names are the same as in
[external-dns-contabo-webhook](https://github.com/hayone1/external-dns-contabo-webhook),
so one Secret can serve both.

Unknown settings are an error, so a typo like `secretNme` is reported
instead of silently ignored. Setting names are not case-sensitive.

There is on purpose **no setting for the Contabo API address** in the issuer.
Whoever can create an issuer could otherwise send your credentials anywhere.

### Ambient credentials

If `secretName` is left out, the webhook uses its own credentials, set by
the cluster admin with the Helm value `ambientCredentials.secretName`. It
does this only when cert-manager allows "ambient credentials" for the
issuer. By default cert-manager allows them for ClusterIssuers and not for
namespaced Issuers (`--issuer-ambient-credentials` changes that).

### Other solver settings that matter

These are cert-manager settings, not webhook settings, but they change what
the webhook is asked to do:

- `cnameStrategy: Follow` (on the `dns01` block): follow a CNAME on
  `_acme-challenge.<domain>` and write the TXT record at its target. See
  [install.md](install.md#use-a-contabo-zone-for-a-domain-hosted-elsewhere).
- `selector.dnsZones` / `selector.dnsNames`: which certificates use this solver.

## 2. Helm values

The full list with comments is in
[charts/cert-manager-webhook-contabo/values.yaml](../charts/cert-manager-webhook-contabo/values.yaml).
The ones you are most likely to change:

| Value | Default | What it does |
|---|---|---|
| `groupName` | `contabo.acme.codejourney.io` | API group of the webhook. Issuers must use the same value. Change it only if another webhook in the cluster uses the same name. |
| `certManager.namespace` | `cert-manager` | Where cert-manager runs. ClusterIssuer Secrets are read here. |
| `certManager.serviceAccountName` | `cert-manager` | cert-manager's ServiceAccount, which is allowed to call the webhook. |
| `secretNames` | `[contabo-credentials]` | Secrets the webhook may read in `certManager.namespace`. |
| `extraSecretAccess` | `[]` | Secrets the webhook may read in other namespaces, for namespaced Issuers: a list of `{namespace, secretNames}`. |
| `ambientCredentials.secretName` | `""` | A Secret in the release namespace with credentials for issuers without `secretName`. |
| `contabo.rateLimit` | `10` | Most API requests per second, per Contabo account. Contabo allows about 15. |
| `contabo.maxRetries` | `4` | Retries for a failed request (server errors, rate limits, network errors) within one call. |
| `contabo.operationTimeout` | `45s` | Time limit for one Present or CleanUp call. Keep it under 60s. |
| `contabo.requestTimeout` | `15s` | Time limit for one HTTP request to Contabo. |
| `logLevel` | `2` | Log detail. `2`: one line per record created or deleted. `4`: every API call. |
| `replicaCount` | `1` | Number of webhook pods. |
| `podDisruptionBudget.enabled` | `false` | Keep at least `minAvailable` pods during node drains. Needs `replicaCount` 2 or more. |
| `image.repository` / `image.tag` / `image.digest` | GHCR, chart version | Which image to run. Use `docker.io/hayone/cert-manager-webhook-contabo` for Docker Hub. |
| `resources`, `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName`, `podSecurityContext`, `securityContext` | safe defaults | The usual pod settings. |
| `pki.caDuration` / `pki.servingCertificateDuration` | `43800h` / `8760h` | Lifetime of the webhook's own HTTPS certificates. cert-manager renews them. |

The chart checks its values: a bad `groupName`, a bad Secret name or a
PodDisruptionBudget that would block node drains fails `helm install` with
a clear message.

### What the chart creates

| Object | Why |
|---|---|
| Deployment, Service, ServiceAccount | The webhook itself. It listens on port 4443 inside the pod; the Service maps 443 to it. |
| APIService `v1alpha1.<groupName>` | Registers the webhook in the Kubernetes API. cert-manager calls it there. |
| Issuers and Certificates (self-signed CA, serving certificate) | HTTPS for the webhook. cert-manager's cainjector puts the CA into the APIService. |
| RoleBinding in `kube-system`, ClusterRoleBinding `system:auth-delegator` | Let the webhook check that callers are allowed, like every API server extension. |
| ClusterRole `<name>:domain-solver` + binding | Lets cert-manager's ServiceAccount call the webhook. |
| Role `<name>:secret-reader` + binding, per namespace | `get` on the listed Secret names only. |
| PodDisruptionBudget | Only when enabled. |

## 3. Environment variables

The chart sets these from the Helm values. You only need them if you deploy
without the chart.

| Variable | Default | What it does |
|---|---|---|
| `GROUP_NAME` | (required) | The API group, same as `groupName`. |
| `CONTABO_API_URL` | `https://api.contabo.com` | Contabo API address. Change it only to test against a fake API. |
| `CONTABO_AUTH_URL` | Contabo's token URL | Contabo login address. Change it only for tests. |
| `CONTABO_CLIENT_ID`, `CONTABO_CLIENT_SECRET`, `CONTABO_API_USER`, `CONTABO_API_PASSWORD` | unset | Ambient credentials. All four or none; three out of four is an error at start. |
| `CONTABO_RATE_LIMIT` | `10` | Requests per second per account, above 0 and at most 15. |
| `CONTABO_MAX_RETRIES` | `4` | 0 to 20. |
| `CONTABO_OPERATION_TIMEOUT` | `45s` | 5s to 10m. |
| `CONTABO_REQUEST_TIMEOUT` | `15s` | 1s to 5m. |

Command-line flags are those of every Kubernetes API server extension, for
example `--secure-port`, `--tls-cert-file`, `--tls-private-key-file` and
`-v`. Run the image with `--help` to see them all. `webhook version` prints
the version.
