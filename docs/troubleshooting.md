# Troubleshooting

## Where to look

Most problems show up in the **Challenge** objects. cert-manager writes the
webhook's error message there.

```sh
kubectl get certificate,certificaterequest,order,challenge -A
kubectl describe challenge -n <namespace> <name>
```

Then the logs:

```sh
# the webhook
kubectl -n cert-manager logs deploy/cert-manager-webhook-contabo
# cert-manager itself
kubectl -n cert-manager logs deploy/cert-manager
```

For more detail, set the Helm value `logLevel=4`: the webhook then logs
every Contabo API call (never the credentials).

## The webhook does not start or is not "Available"

`kubectl get apiservice v1alpha1.contabo.acme.codejourney.io` shows
`AVAILABLE False`:

| Message | Cause and fix |
|---|---|
| `MissingEndpoints` | The webhook pod is not running. Check `kubectl -n cert-manager get pods` and its logs. |
| `FailedDiscoveryCheck` ... `x509: certificate signed by unknown authority` | The CA is not injected yet. cert-manager's **cainjector** must be running. Wait a minute, or check `kubectl -n cert-manager get certificate`. |
| `FailedDiscoveryCheck` ... `context deadline exceeded` | The API server cannot reach the pod. A NetworkPolicy may block port 4443, or the cluster's API server cannot reach pod IPs. |
| Pod log: `GROUP_NAME is required` | You run the image without the chart. Set `GROUP_NAME`. |
| Pod log: `ambient credentials are incomplete` | Only some of the four `CONTABO_*` credential variables are set. Set all four or none. |
| Pod log: `forbidden: User "system:serviceaccount:...` ... `extension-apiserver-authentication` | The RoleBinding in `kube-system` is missing. Reinstall the chart. |

## Challenge errors

| Message in the Challenge | Cause and fix |
|---|---|
| `the server could not find the requested resource` or `no matches for kind` | The issuer's `groupName` does not match the Helm value `groupName`, or `solverName` is not `contabo`. |
| `... is forbidden: User "system:serviceaccount:cert-manager:cert-manager" cannot create resource "contabo"` | cert-manager runs under another ServiceAccount or namespace. Set `certManager.serviceAccountName` and `certManager.namespace`. |
| `the webhook config has no secretName` | Add `secretName` under `config`, or set up [ambient credentials](install.md#one-account-for-all-clusterissuers-ambient-credentials). |
| `Secret <ns>/<name> not found` | The Secret is in the wrong namespace. ClusterIssuer: cert-manager's namespace. Issuer: the Issuer's namespace. |
| `the webhook is not allowed to read Secret <ns>/<name>` | Add the name to `secretNames` (cert-manager's namespace) or to `extraSecretAccess` (other namespaces), then `helm upgrade`. |
| `... is missing these keys or they are empty: ...` | The Secret needs `client-id`, `client-secret`, `api-user`, `api-password`, or set `secretKeys` to your key names. |
| `Contabo rejected the credentials ... the API user or API password is wrong` | Check `api-user` (your e-mail) and `api-password` at [my.contabo.com/api/details](https://my.contabo.com/api/details). |
| `Contabo rejected the credentials ... the client ID or client secret is wrong` | Check `client-id` and `client-secret`. |
| `this Contabo account has no zone that contains ...` | The zone is not in this account (check the account and the spelling), or it was never created in the Contabo panel. |
| `public DNS says ... belongs to the zone ..., but this Contabo account only has ...` | Your domain (or a sub-domain) is served by other name servers. Point the name servers at your domain registrar to Contabo, or see [edge cases](edge-cases.md#the-zone-in-dns-and-the-zone-at-contabo-differ). |
| `Conflicts with pre-existing RRset` | A CNAME already exists at `_acme-challenge.<domain>`. Use `cnameStrategy: Follow` or remove the CNAME. |
| `HTTP 5xx` or `did not finish in 45s` | Contabo is having problems. cert-manager retries on its own. |
| `HTTP 429` | Too many requests. The webhook already slows down; if it keeps happening, lower `contabo.rateLimit`. |
| `invalid webhook config: json: unknown field "..."` | A typo in the issuer's `config`. See [configuration.md](configuration.md#1-issuer-settings). |

## The challenge stays "pending" for a long time

cert-manager is waiting until **all three** Contabo name servers serve the
record. Check it yourself:

```sh
for ns in ns1 ns2 ns3; do dig +short +norec @$ns.contabo.net _acme-challenge.example.com TXT; done
```

- **The value shows on all three:** cert-manager will notice within about
  10 seconds.
- **It shows on some but not all:** a server is still answering from its
  cache. This can take a minute or two ([why](contabo-api.md#dns-01-timing)).
- **It shows nowhere, and the record is in the Contabo panel:** your domain
  probably does not use Contabo's name servers. Check
  `dig +short NS example.com`.
- **The record is not in the Contabo panel:** look at the webhook log for an error.

If cert-manager runs with `--dns01-recursive-nameservers-only`, it asks a
public resolver instead, which may remember "no record" for up to an hour
([details](edge-cases.md#cached-does-not-exist-answers-negative-caching)).

## Let's Encrypt says the challenge failed

Look at `kubectl describe challenge`. Common messages:

- `DNS problem: query timed out looking up TXT`: Let's Encrypt could not
  reach Contabo's name servers at all. Check on [letsdebug.net](https://letsdebug.net)
  and see [edge cases](edge-cases.md#lets-encrypt-cannot-reach-contabos-name-servers).
- `No TXT record found` or `Incorrect TXT record`: DNS changed between
  cert-manager's check and Let's Encrypt's check, or the domain's name
  servers are not Contabo's.
- `CAA record ... prevents issuance`: your domain has a CAA record that does
  not allow `letsencrypt.org`.
- `too many failed authorizations` / `too many certificates`: Let's Encrypt
  rate limits. Wait, and test with the staging server.

## Records are left behind

The webhook deletes its record during CleanUp, and retries when Contabo
fails. If you still find old `_acme-challenge` TXT records with long random
values, see [edge cases](edge-cases.md#records-left-behind-after-a-crash).
They are harmless and can be deleted by hand.

## A challenge cannot be deleted

A Challenge has a finalizer: cert-manager must clean up before it goes
away. If CleanUp keeps failing because the Secret is gone or the webhook is
uninstalled, see [edge cases](edge-cases.md#the-secret-is-deleted-or-the-account-changes-during-a-challenge).

## Still stuck?

Open an [issue](https://github.com/hayone1/cert-manager-webhook-contabo/issues/new/choose)
with the Challenge description and the webhook log (`logLevel=4`).
**Remove all credentials first.**
