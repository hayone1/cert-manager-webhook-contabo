# How it works

## The big picture

```text
 Certificate ──▶ cert-manager ──(Kubernetes API)──▶ webhook ──▶ Contabo API
                     │                                              │
                     │ checks DNS                                   ▼
                     └──────────────▶ ns1/ns2/ns3.contabo.net ◀── Contabo zone
                                              ▲
                                              │ checks DNS
                                        Let's Encrypt
```

1. You create a `Certificate`, or an Ingress with a cert-manager
   annotation.
2. cert-manager asks Let's Encrypt for the certificate. Let's Encrypt
   answers with one **challenge** per name. Each challenge has a one-time
   value, the **challenge key**. The challenge key must appear in a TXT
   record named `_acme-challenge.<name>`, the **challenge record**.
3. cert-manager calls the webhook (**Present**). The webhook adds the
   challenge record through the Contabo API.
4. cert-manager asks the Contabo name servers again and again, until all
   three serve the challenge key. This is the "propagation check".
5. cert-manager tells Let's Encrypt to check. Let's Encrypt also asks the
   Contabo name servers. If it finds the challenge key, it marks the
   challenge as valid.
6. cert-manager calls the webhook again (**CleanUp**). The webhook deletes
   the challenge record.
7. Let's Encrypt issues the certificate. cert-manager stores the
   certificate in a Secret. cert-manager renews it before it expires, by
   default 30 days before.

## How cert-manager reaches the webhook

The webhook is a small **extension of the Kubernetes API server** (an
"aggregated API server"). The Helm chart registers the webhook with an
`APIService` named `v1alpha1.<groupName>`. cert-manager never talks to the
webhook directly. It sends a request to the Kubernetes API server, and the
API server forwards the request to the webhook over HTTPS.

Because of this:

- The webhook needs an HTTPS certificate. cert-manager makes it from a
  small certificate authority (CA) that the chart creates.
- The webhook asks the API server whether each caller is allowed to call
  it (`system:auth-delegator`).
- Only the ServiceAccount of cert-manager may call the webhook
  (`domain-solver` ClusterRole).
- The `groupName` must be unique in the cluster.

## What Present does

1. Reads the `config` of the issuer and checks it.
2. Loads the Contabo API credentials from the Secret in the namespace of
   the challenge. If the issuer names no Secret, Present uses the ambient
   credentials, but only when cert-manager allows them.
3. Gets a Contabo client for that Contabo account from a cache. There is
   one client per Contabo account, so each account has one login token and
   one rate limit.
4. Lists the Contabo zones in the account and picks the Contabo zone for
   the challenge record:
   - the zone that cert-manager found in DNS (`ResolvedZone`), if the
     account has it;
   - otherwise, if cert-manager found no zone, the longest Contabo zone
     name that matches;
   - otherwise an error that explains how DNS and the account disagree.
5. Lists the records of the Contabo zone. If the challenge record already
   exists (with any spelling of the name), Present stops. Nothing more is
   needed.
6. Creates the challenge record with its full name, the challenge key and
   the TTL from the issuer (default 60 seconds). If the Contabo API answers
   "already exists" (HTTP 409), Present also counts that as success.

Present never changes or deletes a record.

## What CleanUp does

1. Steps 1 to 3 are the same as for Present.
2. Looks at every Contabo zone in the account that could hold the name.
   CleanUp does not only look at the zone that Present picked, because the
   setup may have changed in between.
3. Deletes every TXT record with that name whose value is the challenge
   key. CleanUp never touches records with other values.
4. If the challenge record or the Contabo zone is already gone, CleanUp
   counts that as success.
5. If the Contabo API fails (for example HTTP 500 during an outage), the
   webhook tries again a few times, with longer pauses each time. If it
   still fails, CleanUp returns an error, and cert-manager calls CleanUp
   again later.

## Matching names and values

The Contabo API stores names and TXT values exactly as they were sent. So
the same record can look different, depending on the program that created
it:

| Stored name | Meaning |
|---|---|
| `_acme-challenge` | name relative to the zone |
| `_acme-challenge.example.com` | full name |
| `_acme-challenge.example.com.` | full name with a final dot |
| `_ACME-Challenge.Example.com` | the same name; DNS ignores upper and lower case |

| Stored TXT value | Value that DNS serves |
|---|---|
| `abc` | `abc` |
| `"abc"` | `abc` |
| `"ab" "c"` (long values are split) | `abc` |

The webhook understands all of these forms when it looks for the challenge
record. When the webhook writes, it always sends the full name in lower
case and the challenge key without quotes.

## Timing

We measured this on 2026-11-04 with a real Contabo zone:

- A change reaches all three Contabo name servers in **under a second**.
- But each server keeps a copy of its answers for a while (a cache): a "no
  such record" answer for about **60 seconds**, and a real answer for about
  **20 seconds**.
- cert-manager looks up the name before the challenge record exists. So a
  server may keep saying "no such record" for a while after Present.
  Because of that, the propagation check usually passes after **one to two
  minutes**.
- cert-manager handles the challenges for the **same** name one after the
  other. `example.com` and `*.example.com` both use the name
  `_acme-challenge.example.com`. So a certificate for both names takes about
  twice as long as a certificate for one name.

[Contabo API notes](contabo-api.md#dns-01-timing) has all the measured
numbers.

## Safety rules in the code

- Delete only TXT records whose value is the challenge key.
- Never create, change or delete Contabo zones.
- Read Secrets only from the namespace of the challenge.
- Never write secret values or tokens to the log or to error messages.
- Give every call a time limit below the 60-second limit of the Kubernetes
  API server. So if the Contabo API is slow, the call fails cleanly and
  cert-manager tries again. The call never hangs.
