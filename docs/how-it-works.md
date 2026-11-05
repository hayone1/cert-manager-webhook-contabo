# How it works

## The big picture

```text
 Certificate ──▶ cert-manager ──(Kubernetes API)──▶ this webhook ──▶ Contabo API
                     │                                                   │
                     │ checks DNS                                        ▼
                     └──────────────▶ ns1/ns2/ns3.contabo.net ◀── zone records
                                              ▲
                                              │ checks DNS
                                        Let's Encrypt
```

1. You create a `Certificate` (or an Ingress with a cert-manager annotation).
2. cert-manager asks Let's Encrypt for the certificate. Let's Encrypt answers
   with one **challenge** per name: "put this value in a TXT record at
   `_acme-challenge.<name>`".
3. cert-manager calls the webhook (**Present**). The webhook adds the TXT
   record through the Contabo API.
4. cert-manager asks Contabo's name servers, again and again, until all of
   them serve the value. This is the "propagation check".
5. cert-manager tells Let's Encrypt to look. Let's Encrypt asks the name
   servers too and, if the value is there, marks the challenge valid.
6. cert-manager calls the webhook again (**CleanUp**). The webhook deletes
   the TXT record.
7. Let's Encrypt issues the certificate. cert-manager stores it in a Secret
   and renews it before it expires (by default 30 days before).

## How cert-manager reaches the webhook

The webhook is a small **Kubernetes API server extension** (an "aggregated
API server"). The Helm chart registers it with an `APIService` named
`v1alpha1.<groupName>`. cert-manager never talks to the webhook directly: it
sends a request to the Kubernetes API server, which forwards it to the
webhook over HTTPS.

That is why:

- the webhook needs an HTTPS certificate (cert-manager makes it from a small
  CA that the chart creates),
- the webhook checks with the API server that the caller is allowed
  (`system:auth-delegator`),
- only cert-manager's ServiceAccount is allowed to call it (`domain-solver` ClusterRole),
- `groupName` must be unique in the cluster.

## What Present does

1. Reads the issuer's `config` and checks it.
2. Loads the credentials from the Secret in the challenge's namespace (or
   uses ambient credentials when allowed).
3. Gets a Contabo client for that account from a cache. One client per
   account keeps one login token and one rate limit.
4. Lists the zones in the account and picks the zone for the record:
   - the zone cert-manager found in DNS (`ResolvedZone`), when the account has it;
   - otherwise the longest zone name that matches, if cert-manager found no zone;
   - an error, with an explanation, if DNS and the account disagree.
5. Lists the zone's records. If a TXT record with this exact value already
   exists under this name (in any spelling), it stops: the job is done.
6. Creates the TXT record with its full name, the challenge value and a
   60 second TTL. If Contabo answers "already exists" (HTTP 409), that is
   also success.

Present never changes or deletes anything.

## What CleanUp does

1. Same steps 1 to 3 as Present.
2. Looks at every zone in the account that could hold the name (not only the
   one Present would pick today, in case the setup changed in between).
3. Deletes every TXT record under that name whose value is the challenge
   value. Records with other values are never touched.
4. A record that is already gone, or a zone that no longer exists, is
   success.
5. If Contabo fails (for example HTTP 500 during an outage), the webhook
   retries a few times with growing pauses. If it still fails, it returns an
   error, and cert-manager calls CleanUp again later.

## Matching names and values

The Contabo API stores names and TXT values exactly as they were sent, so
the same record can look different depending on who created it:

| Stored name | Means |
|---|---|
| `_acme-challenge` | relative to the zone |
| `_acme-challenge.example.com` | full name |
| `_acme-challenge.example.com.` | full name with a dot |
| `_ACME-Challenge.Example.com` | same name; DNS ignores case |

| Stored TXT value | Served as |
|---|---|
| `abc` | `abc` |
| `"abc"` | `abc` |
| `"ab" "c"` (long values) | `abc` |

The webhook understands all of them when it looks for its record. When it
writes, it always sends the full name in lower case and the plain value.

## Timing

Measured on 2026-11-04 against a real Contabo zone: a change reaches all
three Contabo name servers in **under a second**, but each server caches
its answers, "no such record" for about **60 seconds** and real answers for
about **20 seconds**. Because cert-manager looks up the name before the
record exists, its propagation check usually passes after **one to two
minutes**. cert-manager handles challenges for the *same* name one after
the other, so a certificate for `example.com` plus `*.example.com` takes
about twice as long as one for a single name. See
[contabo-api.md](contabo-api.md#dns-01-timing) for the raw numbers.

## Safety rules the code follows

- Only ever delete TXT records whose value equals the challenge key.
- Never create, change or delete zones.
- Read Secrets only from the challenge's own namespace.
- Never log or return secret values or tokens.
- Every call has a time limit below the Kubernetes API server's 60 second
  limit, so a slow Contabo API turns into a clean retry, not a hang.
