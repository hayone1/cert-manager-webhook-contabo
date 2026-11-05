# Contabo API notes

The [Contabo OpenAPI spec](https://api.contabo.com/api-v1.yaml) does not
tell you everything. These are the things we found by testing the real API,
last checked on **2026-11-04**. The fake server in
`internal/contabo/contabotest` copies all of them, so tests fail if the code
forgets one.

The same notes live in
[external-dns-contabo-webhook](https://github.com/hayone1/external-dns-contabo-webhook/blob/main/docs/contabo-api.md).
Keep both copies in sync. The [DNS-01 section](#dns-01-challenges) at the
end is specific to this project.

## Login

- OAuth2 **password grant** at
  `https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token`
  with `client_id`, `client_secret`, `username` (API user) and `password` (API password).
- The access token lives **300 seconds**. The refresh token lives **1800 seconds**.
  The webhook runs the password grant again instead of using the refresh
  token, so a quiet period longer than 30 minutes never breaks it.
- Wrong user or password: HTTP 400 `invalid_grant`. Wrong client ID or secret: HTTP 401 `unauthorized_client`.

## Every request

- Needs `Authorization: Bearer <token>` and an `x-request-id` header with a
  UUID v4. Without `x-request-id` you get HTTP 400.
- A `DELETE` **without a body** must not send `Content-Type: application/json`,
  or you get HTTP 400 `Body cannot be empty when content-type is set to 'application/json'`.

## Rate limit

Response headers show the limit:

```text
x-ratelimit-limit-second: 15
x-ratelimit-limit-hour: 15000
ratelimit-remaining: 14
ratelimit-reset: 1
```

Too many requests get HTTP 429. The webhook sends at most 10 per second by
default and waits for `ratelimit-reset` when it gets a 429.

## Pagination

- `page` starts at 1. `size` is capped at **1000**; bigger values are cut down.
- `_pagination.totalPages` says how many pages there are. The `_links` values
  are sometimes odd (`page=0`, repeated `size`), so the webhook ignores them.

## Zones

- A new zone already holds 3 `NS` records (`ns1/ns2/ns3.contabo.net`) and one `SOA`.
- Their `prio`, `port`, `weight`, `flag` and `tag` fields are `null`.
- A missing zone is HTTP 404: `Entry DNS Zone not found by zone name = ...`.
- A new zone is not served at once. After the create (HTTP 201), ns1/ns2/ns3
  answered `REFUSED` for the zone for about 3.5 minutes, then all three served
  it (measured 2026-11-04 with `codejourney.io`). Records added after that
  appear within a second, as usual.

## Record names

- The name is stored **exactly as sent**:
  - `www` (relative), `www.example.com` (full) and `www.example.com.` (full
    with dot) all work and are served as the same DNS name.
  - The API also returns them exactly as sent, so a reader must accept all three.
- The bare domain (apex) can be sent as `""`, as the zone name, or by leaving
  `name` out. Contabo's own NS records use the zone name. `@` is refused
  (`Invalid record name: @.example.com`).
- Names are case-insensitive in DNS, but the API keeps the case you sent.
- `PATCH` rewrites the stored name to the full name (`www` becomes `www.example.com`).
- `*` is only allowed as the first label (`*.apps` works, `x.*.apps` does not).

## Record data

- **A / AAAA** must be a public address. These are refused with HTTP 400
  `The data parameter must be valid external IPV4 address ...`:
  private ranges, `100.64.0.0/10`, `198.18.0.0/15`, `240.0.0.0/4`,
  `192.0.2.0/24` and other documentation ranges, `fd00::/8`, `fe80::/10`,
  `2001:db8::/32`. `3fff::/20` (newer IPv6 documentation range) is accepted.
- **TXT** is stored exactly as sent. `"abc"` and `abc` are both served as
  `"abc"` by the name servers. Values longer than 255 characters are split
  into several strings automatically.
- **CNAME** with or without a trailing dot both work.
- **CNAME conflicts:** a CNAME next to any other record of the same name is
  HTTP 400 `Conflicts with pre-existing RRset`. That includes the zone apex,
  which always has NS records.
- **TTL** may be as low as `1` (even `0` is accepted).
- **NS** and **PTR** cannot be created: `type must be one of the following values: A, AAAA, CAA, CNAME, MX, SRV, TXT`.

## Writing

- **Create** a duplicate (same name, type and data): HTTP **409**
  `Entry Record is not unique by name = ...`. The webhook treats this as success.
  The check is smarter than "exact": a relative name, a full name, a full
  name with a dot and an upper-case name all count as the same name, and
  for TXT `abc` and `"abc"` count as the same data (checked 2026-11-04).
- **Update** (`PATCH`) needs `type`, `ttl`, `prio` and `data` every time.
  It does not accept `name`.
- **Update** of a record ID that does not exist: HTTP **500**, not 404.
  The webhook always reads fresh IDs before it updates, so this does not happen.
- **Delete** of a record ID that does not exist: HTTP 204, same as success.
- **Delete can fail for a while.** On 2026-11-03 single and bulk deletes
  answered HTTP 500 (or listed IDs in `failedIds`) for about 90 seconds, then
  worked again. The webhook retries a few times, then returns an error so
  cert-manager calls CleanUp again later.
- **Bulk delete** takes `{"recordIds": [...]}` and answers with `failedIds`.
  Unknown IDs are not reported as failed.

## Errors

The body has one of two shapes:

```json
{"statusCode": 400, "message": "Bad Request ..."}
{"statusCode": 400, "message": ["ttl must be ...", "type must be ..."]}
```

## DNS-01 challenges

Checked on **2026-11-04** with the zone `codetrekker.io` (probe names
`_acme-challenge.probe-cm-<id>.codetrekker.io`, all removed afterwards).

### Several TXT values on one name

- Two TXT records with the same name and different values both exist and
  are both served. This is what a certificate for `example.com` plus
  `*.example.com` needs.
- Deleting one by its record ID leaves the other untouched.

### Names

- A full name that ends in the zone name twice
  (`_acme-challenge.x.codetrekker.io.codetrekker.io`) is stored and served as
  exactly that name. The webhook always sends full names, because a relative
  name like `_acme-challenge.x.codetrekker.io` would be taken as already full.

### SOA

```text
codetrekker.io. 3600 IN SOA ns1.contabo.net. hostmaster.contabo.de. <serial> 10800 3600 604800 3600
```

The last field (3600) tells resolvers how long they may remember a "no such
name" answer. The SOA record cannot be changed through the API.

### DNS-01 timing

How long until the three name servers serve a change, measured with `dig
+norec` against each server every second:

| What | ns1.contabo.net | ns2.contabo.net | ns3.contabo.net |
|---|---|---|---|
| New record, nobody asked for the name before | 0.2 s | 0.2 to 0.5 s | 0.5 to 0.9 s |
| New record, the name was asked for just before creating it | 62 to 150 s | (not asked) | (not asked) |
| New record, first question after 20 s | 21 s (first try) | 0.2 s | 0.5 s |
| Deleted record, after asking while it existed | 6 to 19 s | 0.3 to 17 s | 0.6 to 17 s |

What this means:

- The change itself reaches all three servers in **under a second**.
- Each server **caches its answers**: a "does not exist" answer for about
  **60 seconds** (we saw up to 150), and a real answer for about **20 seconds**.
  These match the default cache times of common authoritative DNS servers.
- So if anyone asks a server for `_acme-challenge.<name>` *before* the record
  exists (cert-manager's own zone lookup, or an early propagation check),
  that server may keep answering "no record" for a minute or two. cert-manager
  checks every 10 seconds and simply waits, so this only adds time.
- In the end-to-end test on a real cluster with Let's Encrypt staging, a
  certificate for one name plus its wildcard took the time shown in
  [development.md](development.md#tests-against-the-real-contabo-api).
