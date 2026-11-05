# Edge cases

This page lists every tricky situation we know of: first the ones the
webhook handles for you, then the ones it **cannot** solve, with what you
can do about them. Each handled case has a test (unit test, conformance
suite or end-to-end test) so it stays handled.

## Handled by the webhook

### Several values on one name

A certificate for `example.com` and `*.example.com` needs two challenges,
and both use the same TXT name, `_acme-challenge.example.com`, with
different values. Other certificates, other clusters or other tools may
also have values there at the same time.

- Present only **adds** its value. It never replaces or removes others.
- CleanUp only deletes records whose value **equals** its own key.
- Unrelated TXT records on the same name (from other tools) are never touched.

Tests: `TestSeveralValuesOnOneName`, conformance `DeletingOneRecordRetainsOthers`,
end-to-end step 1 and the planted `keep-me` record.

### Present or CleanUp runs more than once

cert-manager may call Present again after a timeout or a restart, and
CleanUp more than once.

- Present first looks for its record and does nothing if it is there.
- If two calls race, Contabo answers the second create with HTTP 409
  ("not unique"). That counts as success. On the real API the 409 also
  covers other spellings of the same name and a quoted copy of the value
  (checked 2026-11-04), so races never leave duplicates.
- CleanUp of a record that is already gone is success.

Tests: `TestPresentAndCleanUp`, `TestConflictOnCreateIsSuccess`, `TestConcurrentPresent`.

### Every spelling of a name, and quoted values

Contabo keeps names and values exactly as they were sent (see
[how-it-works.md](how-it-works.md#matching-names-and-values)). The webhook
recognizes relative names, full names, a trailing dot, upper case and
surrounding spaces, and TXT values with or without quotes, split into
several strings, or with escapes. If the same value exists twice under
different spellings (for example made by hand), CleanUp removes both.

Tests: `TestEveryNameFormIsRecognized`, `TestQuotedAndSplitTXTData`, `TestCleanUpRemovesDuplicates`.

### A name that ends in the zone name twice

For `_acme-challenge.example.com.example.com` in the zone `example.com`,
sending the relative name `_acme-challenge.example.com` would make Contabo
think it is already a full name and write the wrong record. The webhook
always sends full names, so this cannot happen. Verified on the real API.

Test: `TestNameEndingInZoneNameTwice`.

### Contabo outages and errors

- **Server errors (HTTP 5xx)** and **network errors**: retried up to 4 times
  with growing, randomized pauses. On 2026-11-03, deletes failed for about
  90 seconds. If the retries run out, the call fails and cert-manager tries
  again later with its own back-off. The record stays until a delete works.
- **Rate limit (HTTP 429)**: the webhook waits as long as Contabo says
  (`Retry-After` or `ratelimit-reset`) and sends at most 10 requests per
  second per account (Contabo allows about 15).
- **Slow API**: each call stops after 45 seconds, before the Kubernetes API
  server's own 60 second limit, and cert-manager retries.
- **Expired or revoked login token**: the webhook logs in again. Tokens last
  only 5 minutes and are renewed 30 seconds early.

Tests: `TestCleanUpDuringDeleteOutage`, `TestRateLimitIsRetried`,
`TestOperationTimeout`, end-to-end outage (25 failing deletes), client tests.

### Finding the right zone

- **Sub-zones**: if the account has both `example.com` and `sub.example.com`,
  a record for `app.sub.example.com` goes into `sub.example.com`.
- **The zone in DNS and the zone at Contabo differ** (see below): the webhook
  refuses with a clear message instead of writing a record no one will see.
- **No matching zone in the account**: Present fails and lists the account's
  zones. CleanUp succeeds (there can be nothing to delete).
- **A zone added a moment ago** is found at once: a miss refreshes the
  cached zone list (normally kept for one minute).
- **A zone deleted during a challenge**: CleanUp succeeds.
- **International domain names** (`bücher.example`) are converted to their
  ASCII form (`xn--bcher-kva.example`).

Tests: `TestSubZoneWins`, `TestResolvedZoneMismatch`, `TestMissingZone`,
`TestZoneAddedLaterIsFound`, `TestZoneRemovedDuringCleanUp`, `TestInternationalDomain`,
end-to-end certificate `child`.

#### The zone in DNS and the zone at Contabo differ

cert-manager finds the zone of a name by asking DNS (`ResolvedZone`).
cert-manager and Let's Encrypt then ask *that* zone's name servers for the
TXT record. If your Contabo account has a different zone, for example:

- the account has `example.com`, but `sub.example.com` is delegated to
  another DNS provider, or
- the account has `sub.example.com`, but nobody delegated it, so DNS still
  answers from `example.com` somewhere else,

then a record written to the Contabo zone would never be seen, and the
challenge would wait forever. The webhook stops with an error that explains
this. If you are sure the webhook is wrong (for example because of unusual
DNS inside your cluster), set `zone` in the issuer config to force a zone.

### CNAME delegation and records at a zone apex

With `cnameStrategy: Follow`, cert-manager follows a CNAME on
`_acme-challenge.<domain>` and asks the webhook to write at the target name.
The target can be any name in any of your Contabo zones, even a zone apex.

Test: `TestCNAMEDelegatedChallenge`.

### Credentials and Secrets

| Situation | What happens |
|---|---|
| Secret missing | Error: "Secret ns/name not found", with where it must be. |
| Keys missing or empty | Error that names the missing keys (never the values). |
| A value ends in a line break (made with `echo`) | The line break is removed. |
| The webhook may not read the Secret | Error that names the Helm value to change (`secretNames` or `extraSecretAccess`). |
| Wrong API user or password | Error: "the API user or API password is wrong". |
| Wrong client ID or secret | Error: "the client ID or client secret is wrong". |
| Secret changed (password rotated) | The next call uses the new values at once. |
| Several issuers with different accounts | One client per account; up to 64 are cached, unused ones are dropped after an hour. |
| An Issuer names a Secret in another namespace | Not possible: the webhook only reads from the Issuer's own namespace. |
| An Issuer without `secretName` | Uses ambient credentials only if cert-manager allows them for that issuer; otherwise a clear error. |
| An issuer tries to change the Contabo API address | Not possible: there is no such issuer setting. |

Tests: `TestSecretProblems`, `TestBadCredentials`, `TestAmbientCredentials`,
`TestClientIsCachedPerAccount`, `TestCacheEvictsIdleClients`, `TestCacheSizeIsBounded`,
end-to-end certificate `namespaced`.

### Bad issuer settings

Typos (`secretNme`), a TTL out of range, a bad Secret name or a bad zone
name are reported as an error on the Challenge, instead of being ignored.

Test: `TestInvalidConfig`.

### Restarts, several replicas, certificate rotation

The webhook keeps no state that matters: if a pod restarts in the middle
of a challenge, cert-manager calls it again and Present or CleanUp simply
finish the job. Several replicas can run at once. The webhook's own HTTPS
certificate is renewed by cert-manager and reloaded without a restart.

### Big zones

Zones with more than 1000 records are read page by page.

## Cannot be solved by the webhook

These depend on things outside the webhook. Here is what happens and what
you can do.

### Your domain does not use Contabo's name servers

Let's Encrypt asks the name servers that your domain registrar lists for
your domain. If those are not `ns1/ns2/ns3.contabo.net`, records in your
Contabo zone are never seen. The webhook detects the common forms of this
(see [above](#the-zone-in-dns-and-the-zone-at-contabo-differ)) and tells you.

**What to do:** set the name servers at your domain registrar to Contabo's,
or use [CNAME delegation](install.md#use-a-contabo-zone-for-a-domain-hosted-elsewhere).

### Contabo's name servers answer from a cache

A new record reaches all three Contabo name servers in under a second, but
each server **remembers its answers**: "no such record" for about 60
seconds (we saw up to 150) and a real answer for about 20 seconds
(measured 2026-11-04, see [contabo-api.md](contabo-api.md#dns-01-timing)).
cert-manager looks up the challenge name before and right after Present,
so a server may keep saying "no record" for a minute or two. cert-manager
checks every 10 seconds and waits until all three servers agree, so you
cannot make this faster from the webhook. Certificates with many names take
longer, because cert-manager handles challenges for the same name one after
the other.

**What to do:** nothing; it works, just not instantly. Do not shorten
cert-manager's check settings because of it.

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

- The TXT record was on all three Contabo name servers within a second.
- Public resolvers (1.1.1.1, 8.8.8.8, 9.9.9.9) and
  [unboundtest.com](https://unboundtest.com) got answers from Contabo's name
  servers.
- Let's Encrypt also timed out looking up the `A` and `AAAA` records of a
  test name in the same zone. So the whole zone was out of reach for Let's
  Encrypt, not only TXT records. HTTP-01 would fail in the same way.
- Domains on other name servers worked.
- A second zone, created at Contabo the same day, failed in the same way
  with fresh TXT and A records. So the problem was not one zone; it was
  Let's Encrypt reaching Contabo's name servers.

Contabo's [status page](https://contabo-status.com) listed work on
"Contabo DNS resolvers" on those days. The problem was gone by 07:07 UTC on
2026-11-05, and a real certificate was issued right after. A certificate authority always asks
the name servers of your domain, so nothing a webhook does can change this.

**What to do:**

1. Check it without cert-manager on [letsdebug.net](https://letsdebug.net)
   (method `dns-01`). It does not need a TXT record: if DNS works, it shows
   no problem. If it shows "query timed out", the problem is between Let's
   Encrypt and Contabo. letsdebug uses its own Let's Encrypt account, so the
   check does not count against your rate limits.
2. Check [contabo-status.com](https://contabo-status.com). If it lasts,
   contact Contabo support: say that Let's Encrypt gets no answer from
   `ns1/ns2/ns3.contabo.net`, and include the letsdebug result.
3. cert-manager retries a failed certificate on its own, after 1 hour, then
   2, 4 and so on. To retry at once when the problem is gone:
   `cmctl renew <certificate> -n <namespace>`, or delete and re-create the
   Certificate.
4. Renewals start 30 days before expiry, so a temporary problem is usually
   absorbed. For important domains, watch for `Ready=False` certificates.
5. If you need a certificate now, try another ACME certificate authority.
   It asks the name servers from its own network, so it may get through
   when Let's Encrypt cannot. We have not tested this during an outage.
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

   Then point the Certificate's `issuerRef.name` at `zerossl-contabo`.

### Long Contabo API outages

If the Contabo API is down for longer than the webhook's retries, the call
fails. cert-manager keeps trying with growing pauses, so the certificate is
only late, not lost. Renewals start 30 days before a certificate expires, so
even a long outage rarely matters.

**What to do:** watch `kubectl get challenges -A`. Check
[contabo-status.com](https://contabo-status.com) if errors mention HTTP 5xx.

### The Secret is deleted, or the account changes, during a challenge

CleanUp needs the same credentials as Present. If the Secret is deleted (or
now points to another Contabo account) before CleanUp, the webhook cannot
remove the record. cert-manager keeps retrying, and the Challenge object
cannot be deleted while it does (it has a finalizer). The leftover TXT
record is harmless (a random value), but it stays.

**What to do:** put the Secret back. Or delete the record by hand in the
Contabo panel and remove the finalizer:
`kubectl patch challenge <name> -n <namespace> --type merge -p '{"metadata":{"finalizers":null}}'`.

### The webhook is uninstalled during a challenge

Same as above: cert-manager cannot call CleanUp. Reinstall the webhook, or
clean up by hand.

### A CNAME already sits on `_acme-challenge` in the Contabo zone

DNS does not allow a TXT record next to a CNAME of the same name. Contabo
answers HTTP 400 "Conflicts with pre-existing RRset", and the webhook shows
that error.

**What to do:** if the CNAME is meant for delegation, add
`cnameStrategy: Follow` to the solver. Otherwise remove the CNAME.

### Records left behind after a crash

If cert-manager loses a challenge without calling CleanUp (for example
because someone removed its finalizer, or cert-manager's data was lost),
the TXT record stays. The webhook has no list of "its" records to sweep,
because it keeps no state and must never guess which records are safe to
delete.

**What to do:** in the Contabo panel, look for TXT records named
`_acme-challenge...` with long random values that are older than an hour,
and delete them. They are not needed after the challenge is over.

### Another tool deletes the TXT record during a challenge

Tools that *replace* all TXT values on a name (instead of adding one) can
remove the webhook's value while Let's Encrypt is checking. The challenge
then fails, and cert-manager starts a new one later.

**What to do:** do not run such tools on `_acme-challenge` names of domains
cert-manager manages.

### Cached "does not exist" answers (negative caching)

The SOA record of every Contabo zone says that resolvers may remember a
"this name does not exist" answer for **3600 seconds**, and the API cannot
change the SOA record. By default cert-manager asks Contabo's name servers
directly, so this does not matter. But if you run cert-manager with
`--dns01-recursive-nameservers-only`, a public resolver that was asked too
early may keep saying "no record" for up to an hour.

**What to do:** keep cert-manager's default (ask the authoritative name
servers). If you must use recursive-only mode, use resolvers that respect
short timeouts, or wait.

### DNS inside your cluster differs from public DNS (split horizon)

cert-manager finds zones and checks records with the DNS servers its pod
uses. If your cluster DNS answers differently than public DNS, cert-manager
may compute the wrong zone or wait for an answer the public will never see.

**What to do:** start cert-manager with
`--dns01-recursive-nameservers=1.1.1.1:53,8.8.8.8:53` so it uses public DNS.

### Let's Encrypt refuses for reasons outside DNS records

- A **CAA record** on your domain that does not allow `letsencrypt.org`.
- **Rate limits**, for example 5 identical certificates per week or 5 failed
  validations per hour. Use the staging server while you test.
- A broken **DNSSEC** setup (a DS record at your registrar that does not match).

**What to do:** read the Order and Challenge status in cert-manager
(`kubectl describe order,challenge -A`); Let's Encrypt's message is there.

### Two webhooks with the same groupName

Two webhooks in one cluster with the same `groupName` overwrite each
other's APIService, and one of them stops working.

**What to do:** give each webhook its own `groupName`. This one defaults to
`contabo.acme.codejourney.io`, which no other project uses.

### No access to the Contabo API from the cluster

The webhook needs outgoing HTTPS to `api.contabo.com` and
`auth.contabo.com`. A NetworkPolicy or firewall that blocks them gives
network errors.

**What to do:** allow that traffic. If you must use a proxy, set
`HTTPS_PROXY` (and `NO_PROXY` for the cluster's own addresses) with the Helm
value `extraEnv`.

### Unknown limits on Contabo's side

Contabo does not document a maximum number of records per zone, or how
TTLs below 60 seconds are honored by its name servers. The rate limit is
about 15 requests per second and 15000 per hour per account; a very large
cluster renewing thousands of certificates in one hour could reach it, and
would then simply be slowed down.

### No test account at Contabo

Contabo has no sandbox, so CI tests against a fake API that copies the real
API's behavior. A change on Contabo's side could break the webhook without
CI noticing. Maintainers run the tests against the real API before each
release ([development.md](development.md#tests-against-the-real-contabo-api))
and update the fake when they find a difference.
