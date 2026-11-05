# Install guide

This guide takes you from an empty cluster to a working wildcard
certificate. You can copy every command as it is. Replace `example.com` and
the credentials with your own values.

## Words used in this guide

- **cert-manager**: a program in your cluster that gets and renews
  certificates.
- **webhook**: this project. cert-manager calls the webhook to change DNS
  records at Contabo.
- **issuer**: a Kubernetes object that tells cert-manager where to get
  certificates (here: Let's Encrypt) and how to prove that you control the
  domain (here: the webhook). There are two kinds. A `ClusterIssuer` works
  in every namespace. An `Issuer` works only in its own namespace.
- **DNS-01 challenge**: the proof that you control the domain. Let's
  Encrypt gives cert-manager a one-time value, the **challenge key**.
- **challenge record**: the TXT record named `_acme-challenge.<domain>`
  that holds the challenge key. Let's Encrypt looks up this record.
- **Contabo zone**: your domain as it is set up in Contabo's DNS, for
  example `example.com`. The Contabo zone holds the DNS records of the
  domain.
- **Contabo name servers**: `ns1.contabo.net`, `ns2.contabo.net` and
  `ns3.contabo.net`. They answer DNS questions about your Contabo zone.
- **groupName**: the name under which the webhook registers in the
  Kubernetes API. Your issuers use the groupName to find the webhook. It is
  only a unique label. You do not need to own it as a domain or set up DNS
  for it. The default is `contabo.acme.codejourney.io`.

## Before you start

You need:

1. A Kubernetes cluster, and `kubectl` and `helm` on your computer.
2. **cert-manager** in the cluster. If cert-manager is not installed yet:

   ```sh
   helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
     --namespace cert-manager --create-namespace \
     --set crds.enabled=true
   ```

3. A domain that uses the **Contabo name servers**. At your domain
   registrar, the name servers of the domain must be `ns1.contabo.net`,
   `ns2.contabo.net` and `ns3.contabo.net`. Check the name servers:

   ```sh
   dig +short NS example.com
   ```

   The output must list the three Contabo name servers. If it lists other
   servers, the certificate cannot work, because Let's Encrypt asks those
   other servers for the challenge record. There is one exception:
   [CNAME delegation](#use-a-contabo-zone-for-a-domain-hosted-elsewhere).

4. A Contabo zone for the domain. Create it in the Contabo control panel
   under **DNS Zone Management**. The webhook never creates or deletes
   Contabo zones. After you create a Contabo zone, the Contabo name
   servers need about 4 minutes before they answer for it.

5. Your **Contabo API credentials**. Open
   [my.contabo.com/api/details](https://my.contabo.com/api/details). You
   need four values: Client ID, Client Secret, API User (your email
   address) and API Password. If you never set an API password, set one
   there first.

## Step 1: install the webhook

Install the webhook into the same namespace as cert-manager:

```sh
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager
```

Check that Kubernetes can reach the webhook. The `AVAILABLE` column must
say `True`. This can take up to a minute, because cert-manager first makes
a certificate for the webhook itself:

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

## Step 2: store your Contabo API credentials

```sh
kubectl -n cert-manager create secret generic contabo-credentials \
  --from-literal=client-id='INT-12345678' \
  --from-literal=client-secret='your-client-secret' \
  --from-literal=api-user='you@example.com' \
  --from-literal=api-password='your-api-password'
```

Use single quotes. Then your shell does not change special characters in
the password.

The webhook can only read the Secrets that you list in the Helm value
`secretNames`. The default list is `contabo-credentials`. If your Secret
has another name, add the name to the list:

```sh
helm upgrade cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager --reuse-values \
  --set 'secretNames={contabo-credentials,my-other-secret}'
```

If you prefer a YAML file, [full examples](configuration.md#full-examples)
shows the same Secret as YAML, a Secret with your own key names, and a
Secret for ambient credentials. An example file is also in
[deploy/examples/secret.example.yaml](../deploy/examples/secret.example.yaml).

## Step 3: create a ClusterIssuer (staging first)

Let's Encrypt has a **staging** server for tests. Browsers do not trust
staging certificates. But staging has high rate limits, so a mistake does
not block you for hours. Always start with staging.

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
    - "*.example.com"   # wildcard: quote it, because * has a special meaning in YAML
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-staging
```

```sh
kubectl apply -f certificate.yaml
kubectl get certificate example-com --watch
```

This usually takes 2 to 5 minutes. Most of that time, cert-manager waits
until all three Contabo name servers show the challenge record. While you
wait, you can watch the challenges:

```sh
kubectl get challenges -A
kubectl describe challenge -n default
```

When `READY` is `True`, the certificate and its key are in the Secret
`example-com-tls`.

## Step 5: switch to production

When staging works, create a production ClusterIssuer:

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

Then change `issuerRef.name` in your Certificate to `letsencrypt-prod`.
cert-manager gets a new certificate that browsers trust.

If you use an Ingress, you can leave out the Certificate. Add this
annotation to the Ingress instead:

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

## Namespaced Issuers (one team, one Contabo account)

An `Issuer` belongs to one namespace. The webhook reads the Secret of an
Issuer **only from that same namespace**. So each team can use its own
Contabo account.

1. Allow the webhook to read the Secret in the namespace of the team. The
   namespace must exist before you run this command:

   ```sh
   helm upgrade cert-manager-webhook-contabo \
     oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
     --namespace cert-manager --reuse-values \
     --set 'extraSecretAccess[0].namespace=team-a' \
     --set 'extraSecretAccess[0].secretNames[0]=contabo-credentials'
   ```

   Or put this in a values file:

   ```yaml
   extraSecretAccess:
     - namespace: team-a
       secretNames: [contabo-credentials]
   ```

2. Create the Secret in the namespace `team-a`. Use the command from step 2
   with `-n team-a`.
3. Create the Issuer in the namespace `team-a`:

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

An Issuer can never use a Secret from another namespace. This is on
purpose: it stops one team from using the Contabo account of another team.

## One Contabo account for all ClusterIssuers ("ambient credentials")

Ambient credentials are credentials that the webhook itself holds. With
ambient credentials, a ClusterIssuer does not need to name a Secret:

```sh
kubectl -n cert-manager create secret generic contabo-ambient \
  --from-literal=client-id='INT-12345678' --from-literal=client-secret='...' \
  --from-literal=api-user='you@example.com' --from-literal=api-password='...'

helm upgrade cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo \
  --namespace cert-manager --reuse-values \
  --set ambientCredentials.secretName=contabo-ambient
```

Then leave `secretName` out of the `config` of the ClusterIssuer.
cert-manager allows ambient credentials only for ClusterIssuers. A
namespaced Issuer still needs its own Secret, unless you start cert-manager
with `--issuer-ambient-credentials`.

## Use a Contabo zone for a domain hosted elsewhere

If the DNS of your domain is **not** at Contabo, you can still use the
webhook with **CNAME delegation**. A CNAME record points one name to
another name.

1. Pick a Contabo zone that you have, for example `acme.example.net`.
2. At the DNS provider of your domain, add a CNAME record from
   `_acme-challenge.example.com` to `example-com.acme.example.net`.
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

cert-manager follows the CNAME record and asks the webhook to write the
challenge record at `example-com.acme.example.net`. Let's Encrypt follows
the same CNAME record.

## Several DNS providers in one cluster

If the DNS of some of your domains is at another provider, use
`selector.dnsZones`. Then each solver handles only its own domains:

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
      # the solver of the other DNS provider
```

## High availability

By default, one webhook pod runs. If the pod is gone for a short time,
cert-manager tries again later, so one pod is enough for most clusters. If
renewals must keep working while you drain nodes, run two pods:

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

Uninstalling does not remove your Secrets, issuers or certificates. The
certificates keep working until they expire. But cert-manager cannot renew
them until you install the webhook (or another DNS-01 solver) again.

## Check the image and chart

Every release is signed. [Check a release](releasing.md#check-a-release)
has the `cosign verify` commands.
