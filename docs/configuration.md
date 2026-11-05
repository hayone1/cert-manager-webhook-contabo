# Configuration reference

You configure the webhook in three places:

1. the **issuer** (`ClusterIssuer` or `Issuer`): which Contabo account the
   webhook uses, and how it writes the challenge record
2. the **Helm values**: how the webhook runs in your cluster
3. the **environment variables** of the webhook pod. The Helm chart sets
   them for you.

The challenge record is the TXT record `_acme-challenge.<domain>` that
holds the challenge key from Let's Encrypt.

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
| `secretKeys.apiUser` | no | `api-user` | Key in the Secret that holds the API user (your email address). |
| `secretKeys.apiPassword` | no | `api-password` | Key in the Secret that holds the API password. |
| `ttl` | no | `60` | Time to live of the challenge record, in seconds, from 1 to 86400. The TTL tells DNS resolvers how long they may keep a copy of the record. Keep it short, because the challenge record exists for only a few minutes. A string such as `"60"` also works. |
| `zone` | no | found automatically | The Contabo zone that the webhook writes to, for example `example.com`. Normally you leave this out. Set it only if the webhook picks the wrong Contabo zone. [Edge cases](edge-cases.md#the-zone-in-dns-and-the-contabo-zone-differ) explains when that happens. |

### Full examples

**Example 1: a Secret with the default key names, and a ClusterIssuer
that uses it.** A ClusterIssuer reads its Secret from the namespace of
cert-manager, so the Secret is in `cert-manager`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: contabo-credentials       # the issuer uses this name in secretName
  namespace: cert-manager         # the namespace of cert-manager
type: Opaque
stringData:                       # plain text; Kubernetes encodes it for you
  client-id: INT-12345678         # Client ID from my.contabo.com/api/details
  client-secret: your-client-secret
  api-user: you@example.com       # API User: your email address
  api-password: your-api-password
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-staging-account
    solvers:
      - dns01:
          webhook:
            groupName: contabo.acme.codejourney.io
            solverName: contabo
            config:
              secretName: contabo-credentials   # the name of the Secret above
```

The Helm value `secretNames` lists `contabo-credentials` by default, so
the webhook may read this Secret. Do not commit a filled-in Secret file to
git. You can also create the same Secret with `kubectl create secret`, as
the [install guide](install.md#step-2-store-your-contabo-api-credentials)
shows.

**Example 2: a Secret with your own key names.** Use `secretKeys` when the
keys in your Secret have other names:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-contabo
  namespace: cert-manager
type: Opaque
stringData:
  id: INT-12345678
  secret: your-client-secret
  user: you@example.com
  password: your-api-password
```

```yaml
# in the ClusterIssuer
config:
  secretName: my-contabo
  secretKeys:
    clientID: id
    clientSecret: secret
    apiUser: user
    apiPassword: password
```

The name `my-contabo` is not in the default `secretNames` list, so allow
it in the Helm values:

```yaml
secretNames:
  - contabo-credentials
  - my-contabo
```

**Example 3: ambient credentials.** The webhook holds one set of
credentials, and the ClusterIssuer names no Secret. The Secret must be in
the namespace where you installed the chart (here `cert-manager`):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: contabo-ambient
  namespace: cert-manager         # the namespace of the Helm release
type: Opaque
stringData:
  client-id: INT-12345678
  client-secret: your-client-secret
  api-user: you@example.com
  api-password: your-api-password
```

```yaml
# Helm values
ambientCredentials:
  secretName: contabo-ambient
```

```yaml
# in the ClusterIssuer: no secretName
solvers:
  - dns01:
      webhook:
        groupName: contabo.acme.codejourney.io
        solverName: contabo
        config: {}
```

**Example 4: an Issuer in its own namespace.** The
[install guide](install.md#namespaced-issuers-one-team-one-contabo-account)
shows the Secret, the Helm value `extraSecretAccess` and the Issuer.

More example files are in [deploy/examples](../deploy/examples).

The default key names are the same as in
[external-dns-contabo-webhook](https://github.com/hayone1/external-dns-contabo-webhook),
so one Secret can serve both projects.

An unknown setting is an error. So a typo such as `secretNme` gives an
error message and is not ignored. Setting names are not case-sensitive.

The issuer has **no setting for the Contabo API address**, on purpose.
With such a setting, anyone who can create an issuer could send your
Contabo API credentials to their own server.

### Ambient credentials

Ambient credentials are credentials that the webhook itself holds. The
cluster admin sets them with the Helm value `ambientCredentials.secretName`.
If an issuer has no `secretName`, the webhook uses the ambient credentials.
It does this only when cert-manager allows ambient credentials for that
issuer. By default, cert-manager allows them for ClusterIssuers and not for
namespaced Issuers. The cert-manager flag `--issuer-ambient-credentials`
changes that.

### cert-manager settings that change what the webhook does

These are cert-manager settings, not webhook settings. But they change
what cert-manager asks the webhook to do:

- `cnameStrategy: Follow` (in the `dns01` block): if
  `_acme-challenge.<domain>` is a CNAME record, cert-manager follows it, and
  the webhook writes the challenge record at the target name. See
  [CNAME delegation](install.md#use-a-contabo-zone-for-a-domain-hosted-elsewhere).
- `selector.dnsZones` and `selector.dnsNames`: which certificates use this
  solver.

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
| `resources`, `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName`, `podSecurityContext`, `securityContext` | see `values.yaml` | Standard Kubernetes pod settings. The defaults run the pod as non-root, with a read-only file system and no extra Linux capabilities. |
| `pki.caDuration` / `pki.servingCertificateDuration` | `43800h` / `8760h` | How long the HTTPS certificates of the webhook itself are valid. cert-manager renews them. |

The chart checks its values. `helm install` stops with an error message
in these cases: a `groupName` that is not a valid name, a Secret name that
is not valid, or a PodDisruptionBudget that would block node drains.

### What the chart creates

| Object | Why |
|---|---|
| Deployment, Service, ServiceAccount | The webhook itself. It listens on port 4443 inside the pod; the Service maps 443 to it. |
| APIService `v1alpha1.<groupName>` | Registers the webhook in the Kubernetes API. cert-manager calls it there. |
| Issuers and Certificates (self-signed CA, serving certificate) | HTTPS for the webhook. cert-manager's cainjector puts the CA into the APIService. |
| RoleBinding in `kube-system`, ClusterRoleBinding `system:auth-delegator` | Let the webhook check that each caller is allowed to call it. Every Kubernetes API extension needs these. |
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
