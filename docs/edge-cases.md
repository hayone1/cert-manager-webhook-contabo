# Edge cases

This page lists every unusual case that we know of. The first part lists
the cases that the webhook handles for you. The second part lists the cases
that the webhook **cannot** solve, and what you can do about them. Each
handled case has a test (a unit test, the conformance tests or the
end-to-end test), so it stays handled.

Words used on this page:

- **challenge key**: the one-time value that Let's Encrypt gives
  cert-manager for one challenge.
- **challenge record**: the TXT record `_acme-challenge.<domain>` that
  holds the challenge key.
- **Contabo zone**: a domain as it is set up in Contabo's DNS, with all its
  records.
- **Contabo name servers**: `ns1.contabo.net`, `ns2.contabo.net` and
  `ns3.contabo.net`.

## Handled by the webhook

### Several values on one name

A certificate for `example.com` and `*.example.com` needs two challenges.
Both challenges use the same name, `_acme-challenge.example.com`, but each
has its own challenge key. Other certificates, other clusters or other
tools may also have TXT records with that name at the same time.

- Present only **adds** a TXT record. It never replaces or removes other
  TXT records.
- CleanUp only deletes TXT records whose value **equals** its own challenge
  key.
- Present and CleanUp never touch TXT records of other tools on the same
  name.

Tests: `TestSeveralValuesOnOneName`, conformance `DeletingOneRecordRetainsOthers`,
end-to-end step 1 and the planted `keep-me` record.

### Present or CleanUp runs more than once

cert-manager may call Present again after a timeout or a restart. It may
also call CleanUp more than once.

- Present first looks for the challenge record. If the challenge record
  exists, Present does nothing more.
- Two Present calls can run at the same time. Then the Contabo API answers
  the second create with HTTP 409 ("not unique"), and Present counts that
  as success. The real Contabo API also answers 409 for another spelling of
  the same name and for a quoted copy of the same value (checked
  2026-11-04). So two calls never leave two copies of the challenge record.
- If the challenge record is already gone, CleanUp counts that as success.

Tests: `TestPresentAndCleanUp`, `TestConflictOnCreateIsSuccess`, `TestConcurrentPresent`.

### Every spelling of a name, and quoted values

The Contabo API stores names and values exactly as they were sent (see
[matching names and values](how-it-works.md#matching-names-and-values)).
The webhook recognizes all these forms of a name: relative names, full
names, a final dot, upper case and spaces around the name. It also
recognizes TXT values with or without quotes, values split into several
strings, and values with escape characters. If the challenge key exists
twice under different spellings of the name (for example, made by hand),
CleanUp removes both.

Tests: `TestEveryNameFormIsRecognized`, `TestQuotedAndSplitTXTData`, `TestCleanUpRemovesDuplicates`.

### A name that ends in the zone name twice

Take the name `_acme-challenge.example.com.example.com` in the Contabo
zone `example.com`. Its relative name is `_acme-challenge.example.com`. If
the webhook sent that relative name, the Contabo API would think it is
already a full name and write the wrong record. The webhook always sends
full names, so this cannot happen. We checked this on the real Contabo
API.

Test: `TestNameEndingInZoneNameTwice`.

### Contabo outages and errors

- **Server errors (HTTP 5xx)** and **network errors**: the webhook tries
  again up to 4 times. Each pause is longer than the one before, plus a
  random part. On 2026-11-03, deletes failed for about 90 seconds. If all
  tries fail, the call fails, and cert-manager tries again later with its
  own longer pauses. The challenge record stays until a delete works.
- **Rate limit (HTTP 429)**: the webhook waits as long as the Contabo API
  says (`Retry-After` or `ratelimit-reset`). It sends at most 10 requests
  per second per Contabo account. The Contabo API allows about 15.
- **Slow Contabo API**: each call stops after 45 seconds. That is before
  the 60-second limit of the Kubernetes API server. cert-manager then tries
  again.
- **Expired or revoked login token**: the webhook logs in again. A login
  token lasts only 5 minutes. The webhook gets a new one 30 seconds before
  the old one expires.

Tests: `TestCleanUpDuringDeleteOutage`, `TestRateLimitIsRetried`,
`TestOperationTimeout`, end-to-end outage (25 failing deletes), client tests.

### Finding the right zone

- **Sub-zones**: if the account has both `example.com` and
  `sub.example.com`, a record for `app.sub.example.com` goes into
  `sub.example.com`.
- **The zone in DNS and the Contabo zone differ** (see below): the webhook
  stops with an error message. It does not write a record that nobody can
  see.
- **No matching Contabo zone in the account**: Present fails, and the error
  message lists the Contabo zones of the account. CleanUp succeeds, because
  there is nothing to delete.
- **A Contabo zone added a moment ago**: the webhook finds it at once. The
  webhook keeps the list of Contabo zones for one minute, but it reloads
  the list when a name matches no zone.
- **A Contabo zone deleted during a challenge**: CleanUp succeeds.
- **International domain names** (`bücher.example`) are converted to their
  ASCII form (`xn--bcher-kva.example`).

Tests: `TestSubZoneWins`, `TestResolvedZoneMismatch`, `TestMissingZone`,
`TestZoneAddedLaterIsFound`, `TestZoneRemovedDuringCleanUp`, `TestInternationalDomain`,
end-to-end certificate `child`.

#### The zone in DNS and the Contabo zone differ

cert-manager finds the zone of a name by asking DNS. cert-manager calls
this zone the `ResolvedZone`. cert-manager and Let's Encrypt then ask the
name servers of **that** zone for the challenge record. Your Contabo
account can have a different zone. Two examples:

- The account has `example.com`, but `sub.example.com` is delegated to
  another DNS provider.
- The account has `sub.example.com`, but nobody delegated it to Contabo.
  So DNS still answers for it from `example.com` at another provider.

In both cases, nobody would ever see a record in the Contabo zone, and the
challenge would wait forever. So the webhook stops with an error message
that explains this. You may be sure that the webhook is wrong, for example
because DNS inside your cluster is unusual. Then set `zone` in the issuer
config to choose the Contabo zone yourself.

### CNAME delegation and records at a zone apex

With `cnameStrategy: Follow`, cert-manager follows a CNAME record at
`_acme-challenge.<domain>`. It asks the webhook to write the challenge
record at the target name. The target can be any name in any of your
Contabo zones. It can even be the apex of a zone (the zone name itself,
such as `example.com`).

Test: `TestCNAMEDelegatedChallenge`.

### Credentials and Secrets

| Situation | What happens |
|---|---|
| Secret missing | Error: "Secret ns/name not found". The message says in which namespace the Secret must be. |
| Keys missing or empty | Error that names the missing keys (never the values). |
| A value ends in a line break (made with `echo`) | The line break is removed. |
| The webhook may not read the Secret | Error that names the Helm value to change (`secretNames` or `extraSecretAccess`). |
| Wrong API user or password | Error: "the API user or API password is wrong". |
| Wrong client ID or secret | Error: "the client ID or client secret is wrong". |
| Secret changed (password rotated) | The next call uses the new values at once. |
| Several issuers with different accounts | One Contabo client per account. The webhook keeps up to 64 clients. It drops a client that was not used for an hour. |
| An Issuer names a Secret in another namespace | Not possible: the webhook only reads from the Issuer's own namespace. |
| An Issuer without `secretName` | Uses ambient credentials, but only if cert-manager allows them for that issuer. Otherwise the webhook returns an error that says so. |
| An issuer tries to change the Contabo API address | Not possible: there is no such issuer setting. |

Tests: `TestSecretProblems`, `TestBadCredentials`, `TestAmbientCredentials`,
`TestClientIsCachedPerAccount`, `TestCacheEvictsIdleClients`, `TestCacheSizeIsBounded`,
end-to-end certificate `namespaced`.

### Bad issuer settings

These settings give an error on the Challenge. The webhook does not
ignore them: a typo such as `secretNme`, a TTL out of range, a Secret name
that is not valid, or a zone name that is not valid.

Test: `TestInvalidConfig`.

### Restarts, several replicas, certificate rotation

The webhook stores nothing that it needs later. If a pod restarts in the
middle of a challenge, cert-manager calls the webhook again, and Present or
CleanUp finishes the job. Several webhook pods can run at the same time.
cert-manager renews the HTTPS certificate of the webhook itself, and the
webhook loads the new certificate without a restart.

### Big zones

The webhook reads Contabo zones with more than 1000 records page by
page.

## Cannot be solved by the webhook

These cases depend on things outside the webhook. Each case says what
happens and what you can do.

### Your domain does not use Contabo's name servers

Let's Encrypt asks the name servers that your domain registrar lists for
your domain. If those are not the Contabo name servers, nobody sees the
records in your Contabo zone. The webhook finds the common forms of this
problem (see [above](#the-zone-in-dns-and-the-contabo-zone-differ)) and
tells you in the error message.

**What to do:** at your domain registrar, set the name servers of the
domain to the Contabo name servers,
or use [CNAME delegation](install.md#use-a-contabo-zone-for-a-domain-hosted-elsewhere).

### The Contabo name servers answer from a cache

A new record reaches all three Contabo name servers in under a second. But
each server **keeps a copy of its answers** for a while: a "no such
record" answer for about 60 seconds (we saw up to 150), and a real answer
for about 20 seconds. We measured this on 2026-11-04 (see
[DNS-01 timing](contabo-api.md#dns-01-timing)).

cert-manager looks up the challenge record before Present and right after
Present. So a server may keep saying "no such record" for a minute or two.
cert-manager checks every 10 seconds and waits until all three servers
show the challenge record. The webhook cannot make this faster.
Certificates with many names take longer, because cert-manager handles the
challenges for the same name one after the other.

**What to do:** nothing. The certificate comes, but not at once. Do not
shorten the check settings of cert-manager because of this.

### Let's Encrypt cannot reach Contabo's name servers

On 2026-11-04 every Let's Encrypt (staging) validation for our test zone
failed with:

```text
DNS problem: query timed out looking up TXT for _acme-challenge.<name>
```

"Timed out" means Let's Encrypt got **no answer at all**. That is not the
same as "No TXT record found", which is what you get when the name servers
answer but the record is missing.

What we checked that day:

- The challenge record was on all three Contabo name servers within a
  second.
- Public resolvers (1.1.1.1, 8.8.8.8, 9.9.9.9) and
  [unboundtest.com](https://unboundtest.com) got answers from the Contabo
  name servers.
- Let's Encrypt also timed out looking up the `A` and `AAAA` records of a
  test name in the same zone. So the whole zone was out of reach for Let's
  Encrypt, not only TXT records. HTTP-01 would fail in the same way.
- Domains on other name servers worked.
- A second Contabo zone, created the same day, failed in the same way
  with new TXT and A records. So the problem was not one zone. The problem
  was the connection from Let's Encrypt to the Contabo name servers.

The Contabo [status page](https://contabo-status.com) listed work on
"Contabo DNS resolvers" on those days. The problem was gone by 07:07 UTC on
2026-11-05, and we got a real certificate right after that. A certificate
authority always asks the name servers of your domain. So nothing that a
webhook does can change this problem.

**What to do:**

1. Check your domain without cert-manager on
   [letsdebug.net](https://letsdebug.net) (method `dns-01`). letsdebug does
   not need a challenge record: if DNS works, it shows no problem. If it
   shows "query timed out", the problem is between Let's Encrypt and
   Contabo. letsdebug uses its own Let's Encrypt account, so the check does
   not count against your rate limits. If letsdebug shows "ServiceBusy",
   its Let's Encrypt check did not run. Run the test again.
2. Check [contabo-status.com](https://contabo-status.com). If the
   problem lasts, contact Contabo support. Say that Let's Encrypt gets no
   answer from `ns1/ns2/ns3.contabo.net`, and include the letsdebug result.
3. cert-manager tries a failed certificate again on its own: after 1 hour,
   then after 2 hours, 4 hours and so on. To try again at once when the
   problem is gone:
   `cmctl renew <certificate> -n <namespace>`, or delete and re-create the
   Certificate.
4. Renewals start 30 days before a certificate expires. So a short
   problem usually does no harm. For important domains, watch for
   certificates with `Ready=False`.
5. If you need a certificate now, try another ACME certificate authority.
   That certificate authority asks the name servers from its own network,
   so it may get an answer when Let's Encrypt cannot. We have not tested this during an outage.
   Many such authorities need External Account Binding (EAB): a key ID and
   a key from your account with them. Put the key in a Secret in the
   `cert-manager` namespace, then add a second ClusterIssuer that uses the
   same webhook. Example for ZeroSSL:

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata:
     name: zerossl-contabo
   spec:
     acme:
       server: https://acme.zerossl.com/v2/DV90
       externalAccountBinding:
         keyID: YOUR_EAB_KEY_ID
         keySecretRef:
           name: zerossl-eab   # Secret with the EAB key under "secret"
           key: secret
       privateKeySecretRef:
         name: zerossl-account
       solvers:
         - dns01:
             webhook:
               groupName: contabo.acme.codejourney.io
               solverName: contabo
               config:
                 secretName: contabo-credentials
   ```

   Then set `issuerRef.name` in the Certificate to `zerossl-contabo`.

### Long Contabo API outages

If the Contabo API is down for longer than all the tries of the webhook,
the call fails. cert-manager keeps trying, with longer and longer pauses.
So the certificate comes late, but it comes. Renewals start 30 days before
a certificate expires, so even a long outage rarely matters.

**What to do:** watch `kubectl get challenges -A`. Check
[contabo-status.com](https://contabo-status.com) if errors mention HTTP 5xx.

### The Secret is deleted, or the account changes, during a challenge

CleanUp needs the same credentials as Present. Someone may delete the
Secret, or change it to another Contabo account, before CleanUp runs. Then
the webhook cannot remove the challenge record. cert-manager keeps trying.
While it tries, you cannot delete the Challenge object, because the
Challenge has a finalizer. (A finalizer is a mark that makes Kubernetes
wait for cleanup before it deletes an object.) The challenge record that
stays behind does no harm, because it only holds a random value. But it
stays.

**What to do:** put the Secret back. Or delete the challenge record by
hand in the Contabo panel, and then remove the finalizer:
`kubectl patch challenge <name> -n <namespace> --type merge -p '{"metadata":{"finalizers":null}}'`.

### The webhook is uninstalled during a challenge

This is the same problem as above: cert-manager cannot call CleanUp.
Install the webhook again, or clean up by hand as described above.

### A CNAME already sits on `_acme-challenge` in the Contabo zone

DNS does not allow a TXT record and a CNAME record with the same name.
The Contabo API answers HTTP 400 "Conflicts with pre-existing RRset", and
the webhook shows that error.

**What to do:** if the CNAME record is there for CNAME delegation, add
`cnameStrategy: Follow` to the solver. Otherwise remove the CNAME record.

### Records left behind after a crash

cert-manager may lose a challenge without calling CleanUp. For example,
someone removed the finalizer of the Challenge, or the data of
cert-manager was lost. Then the challenge record stays. The webhook keeps
no list of the records that it created. It must never guess which records
are safe to delete, so it cannot clean up these records later.

**What to do:** in the Contabo panel, look for TXT records named
`_acme-challenge...` with long random values that are older than an hour.
Delete them. Nobody needs a challenge record after its challenge is over.

### Another tool deletes the TXT record during a challenge

Some tools **replace** all TXT records of a name instead of adding one.
Such a tool can remove the challenge record while Let's Encrypt checks it.
The challenge then fails, and cert-manager starts a new challenge later.

**What to do:** do not let such tools change `_acme-challenge` names of
domains that cert-manager manages.

### Cached "does not exist" answers (negative caching)

The SOA record of every Contabo zone says that resolvers may keep a "this
name does not exist" answer for **3600 seconds**. The Contabo API cannot
change the SOA record. By default, cert-manager asks the Contabo name
servers directly, so this does not matter. But you may run cert-manager
with `--dns01-recursive-nameservers-only`. Then cert-manager asks a public
resolver. If that resolver was asked before the challenge record existed,
it may keep saying "no such record" for up to an hour.

**What to do:** keep the default of cert-manager, which asks the Contabo
name servers directly. If you must use `--dns01-recursive-nameservers-only`,
use resolvers that keep "does not exist" answers only for a short time, or
wait.

### DNS inside your cluster differs from public DNS (split horizon)

cert-manager finds zones and checks records with the DNS servers that its
pod uses. The DNS of your cluster may answer differently than public DNS.
Then cert-manager may pick the wrong zone, or wait for an answer that
public DNS never gives.

**What to do:** start cert-manager with
`--dns01-recursive-nameservers=1.1.1.1:53,8.8.8.8:53`. Then cert-manager
uses public DNS.

### Let's Encrypt refuses for reasons outside DNS records

- A **CAA record** on your domain that does not allow `letsencrypt.org`.
  A CAA record lists the certificate authorities that may issue
  certificates for the domain.
- **Rate limits**, for example 5 identical certificates per week, or 5
  failed checks per hour. Use the staging server while you test.
- A broken **DNSSEC** setup, for example a DS record at your domain
  registrar that does not match the zone.

**What to do:** read the status of the Order and the Challenge in
cert-manager (`kubectl describe order,challenge -A`). The message from
Let's Encrypt is there.

### Two webhooks with the same groupName

If two webhooks in one cluster have the same `groupName`, each one
overwrites the APIService of the other. Then one of them stops working.

**What to do:** give each webhook its own `groupName`. The default
`groupName` of this webhook is `contabo.acme.codejourney.io`. No other
project uses this name.

### No access to the Contabo API from the cluster

The webhook needs outgoing HTTPS connections to `api.contabo.com` and
`auth.contabo.com`. If a NetworkPolicy or a firewall blocks them, the
webhook gets network errors.

**What to do:** allow these connections. If you must use a proxy, set
`HTTPS_PROXY` with the Helm value `extraEnv`. Also set `NO_PROXY` to the
addresses inside your cluster.

### Unknown limits on Contabo's side

Contabo does not document a maximum number of records per zone. It also
does not document how its name servers handle TTLs below 60 seconds. The
rate limit is about 15 requests per second and 15000 requests per hour per
Contabo account. A very large cluster that renews thousands of
certificates in one hour could reach this limit. The webhook would then
only get slower; it would not fail.

### No test account at Contabo

Contabo has no test system (sandbox). So CI tests against a fake Contabo
API that copies the behavior of the real Contabo API. A change on Contabo's
side could break the webhook, and CI would not notice. Before each release,
maintainers run the tests against the real Contabo API (see
[tests against the real Contabo API](development.md#tests-against-the-real-contabo-api)).
When they find a difference, they update the fake Contabo API.
