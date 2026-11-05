# Install guide

This guide takes you from nothing to a working wildcard certificate. Every
command can be copied as it is; replace `example.com` and the credentials
with your own.

## Words used in this guide

- **cert-manager**: a program in your cluster that gets and renews certificates.
- **Issuer / ClusterIssuer**: tells cert-manager *where* to get certificates
  (here: Let's Encrypt) and *how* to prove you own the domain (here: this
  webhook). A `ClusterIssuer` works in every namespace. An `Issuer` works
  only in its own namespace.
- **DNS-01 challenge**: the proof. A TXT record named
  `_acme-challenge.<domain>` holds a one-time value that Let's Encrypt checks.
- **Zone**: your domain as it is set up in Contabo's DNS (for example `example.com`).
- **groupName**: the name the webhook registers in the Kubernetes API. Your
  issuers use it to find the webhook. It is not a DNS name you need to own
  or set up; it is just a unique label. The default is
  `contabo.acme.codejourney.io`.

## Before you start

You need:

1. A Kubernetes cluster and `kubectl` and `helm` on your computer.
2. **cert-manager** in the cluster. If it is not installed yet:

   ```sh
   helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
     --namespace cert-manager --create-namespace \
     --set crds.enabled=true
   ```

3. A domain that uses **Contabo's name servers**. At your domain registrar,
   the name servers must be `ns1.contabo.net`, `ns2.contabo.net` and
   `ns3.contabo.net`. Check it:

   ```sh
   dig +short NS example.com
   ```

   You should see the three Contabo servers. If you see others, the
   certificate cannot work: Let's Encrypt would ask those other servers.
   (There is one exception, see [CNAME delegation](#use-a-contabo-zone-for-a-domain-hosted-elsewhere).)

4. The zone in the Contabo control panel (**DNS Zone Management**). The
   webhook never creates or deletes zones.

5. Your **Contabo API credentials**. Open
   [my.contabo.com/api/details](https://my.contabo.com/api/details). You need
   four values: Client ID, Client Secret, API User (your e-mail) and API
   Password. If you never set an API password, set one there first.

## Step 1: install the webhook

Install it into the same namespace as cert-manager:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

Check that Kubernetes can reach it. `AVAILABLE` must say `True` (this can
take up to a minute, while cert-manager makes the webhook's own certificate):

```sh
kubectl get apiservice v1alpha1.contabo.acme.codejourney.io
```

If cert-manager runs in another namespace, or under another ServiceAccount
name, tell the chart:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace my-cert-manager \
  --set certManager.namespace=my-cert-manager \
  --set certManager.serviceAccountName=my-cert-manager
```

## Step 2: store your Contabo credentials

```sh
kubectl -n cert-manager create secret generic contabo-credentials \
  --from-literal=client-id='INT-12345678' \
  --from-literal=client-secret='your-client-secret' \
  --from-literal=api-user='you@example.com' \
  --from-literal=api-password='your-api-password'
```

Use single quotes, so your shell does not change special characters in the
password.

The webhook may only read Secrets you name in the Helm value `secretNames`
(default: `contabo-credentials`). If you use another name, add it:

```sh
helm upgrade cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager --reuse-values \
  --set 'secretNames={contabo-credentials,my-other-secret}'
```

A ready-made file is in [deploy/examples/secret.yaml](../deploy/examples/secret.yaml).

## Step 3: create a ClusterIssuer (staging first)

Let's Encrypt has a **staging** server for tests. Its certificates are not
trusted by browsers, but its limits are high, so mistakes do not lock you
out. Always start there.

```yaml
# letsencrypt-staging.yaml
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
              secretName: contabo-credentials
```

```sh
kubectl apply -f letsencrypt-staging.yaml
kubectl get clusterissuer letsencrypt-staging   # READY must be True
```

## Step 4: get a certificate

```yaml
# certificate.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  dnsNames:
    - example.com
    - "*.example.com"   # wildcard: quote it, YAML treats * as special
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-staging
```

```sh
kubectl apply -f certificate.yaml
kubectl get certificate example-com --watch
```

It usually takes one to three minutes. While you wait, you can watch the
challenges:

```sh
kubectl get challenges -A
kubectl describe challenge -n default
```

When `READY` is `True`, the certificate and key are in the Secret
`example-com-tls`.

## Step 5: switch to production

When staging works, create the production issuer and point your
certificates at it:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-prod-account
    solvers:
      - dns01:
          webhook:
            groupName: contabo.acme.codejourney.io
            solverName: contabo
            config:
              secretName: contabo-credentials
```

Change `issuerRef.name` in your Certificate to `letsencrypt-prod`.
cert-manager gets a new, trusted certificate.

With an Ingress you can skip the Certificate and add an annotation instead:

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

## Namespaced Issuers (one team, one account)

An `Issuer` lives in one namespace and reads its Secret **from that same
namespace**. This lets each team use its own Contabo account.

1. Allow the webhook to read the Secret in that namespace. The namespace
   must exist first:

   ```sh
   helm upgrade cert-manager-webhook-contabo \
     oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
     --namespace cert-manager --reuse-values \
     --set 'extraSecretAccess[0].namespace=team-a' \
     --set 'extraSecretAccess[0].secretNames[0]=contabo-credentials'
   ```

   Or in a values file:

   ```yaml
   extraSecretAccess:
     - namespace: team-a
       secretNames: [contabo-credentials]
   ```

2. Create the Secret in `team-a` (same command as step 2, with `-n team-a`).
3. Create the Issuer in `team-a`:

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Issuer
   metadata:
     name: letsencrypt-staging
     namespace: team-a
   spec:
     acme:
       server: https://acme-staging-v02.api.letsencrypt.org/directory
       email: team-a@example.com
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

An Issuer can never read a Secret from another namespace. That is on
purpose: it stops one team from using another team's account.

## One account for all ClusterIssuers ("ambient credentials")

Instead of naming a Secret in every ClusterIssuer, you can give the webhook
one set of credentials:

```sh
kubectl -n cert-manager create secret generic contabo-ambient \
  --from-literal=client-id='INT-12345678' --from-literal=client-secret='...' \
  --from-literal=api-user='you@example.com' --from-literal=api-password='...'

helm upgrade cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager --reuse-values \
  --set ambientCredentials.secretName=contabo-ambient
```

Then leave `secretName` out of the issuer's `config`. cert-manager only
allows this for ClusterIssuers (unless you start cert-manager with
`--issuer-ambient-credentials`), so namespaced Issuers still need their own
Secret.

## Use a Contabo zone for a domain hosted elsewhere

If your domain's DNS is *not* at Contabo, you can still use this webhook
with **CNAME delegation**:

1. Pick a zone you do have at Contabo, for example `acme.example.net`.
2. At your current DNS provider, add a CNAME:
   `_acme-challenge.example.com` → `example-com.acme.example.net`.
3. Add `cnameStrategy: Follow` to the solver:

   ```yaml
   solvers:
     - dns01:
         cnameStrategy: Follow
         webhook:
           groupName: contabo.acme.codejourney.io
           solverName: contabo
           config:
             secretName: contabo-credentials
   ```

cert-manager follows the CNAME and asks the webhook to write the TXT record
at `example-com.acme.example.net`. Let's Encrypt follows the same CNAME.

## Several DNS providers in one cluster

If some of your domains are elsewhere, use `selector.dnsZones` so each
solver handles only its own domains:

```yaml
solvers:
  - selector:
      dnsZones: ["example.com"]   # also matches every name below example.com
    dns01:
      webhook:
        groupName: contabo.acme.codejourney.io
        solverName: contabo
        config:
          secretName: contabo-credentials
  - selector:
      dnsZones: ["example.org"]
    dns01:
      # another provider's solver
```

## High availability

By default one webhook pod runs. cert-manager retries when the webhook is
briefly gone, so one pod is fine for most clusters. If you want renewals to
keep working while nodes are drained:

```yaml
replicaCount: 2
podDisruptionBudget:
  enabled: true
  minAvailable: 1
```

## Upgrade and uninstall

```sh
helm upgrade cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager --reuse-values

helm uninstall cert-manager-webhook-contabo --namespace cert-manager
```

Uninstalling does not remove your Secrets, issuers or certificates. Existing
certificates keep working, but they cannot be renewed until a DNS-01 solver
is back.

## Check the image and chart

Every release is signed. See [releasing.md](releasing.md#check-a-release)
for the `cosign verify` commands.
