---
name: probe-contabo-api
description: Safely check how the real Contabo DNS API and name servers behave (name formats, duplicate rules, TXT quoting, propagation and caching) and record the findings in the fake API and docs. Use when a test against the fake passes but the real API behaves differently, or before relying on an undocumented behavior.
---

# Probe the Contabo API

## Rules

- Use a zone the user named. Never create or delete zones.
- Use record names that start with `probe-cm-` so they are easy to find and
  remove (external-dns-contabo-webhook uses `probe-`).
- Never print tokens or credentials. Load them from `.env` into the environment.
- Remove every probe record when you are done, and show the user the final
  record count.

## Helper

```sh
set -a; . ./.env; set +a
TOKEN=$(curl -sSf https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token \
  --data-urlencode grant_type=password \
  --data-urlencode "client_id=$CONTABO_CLIENT_ID" --data-urlencode "client_secret=$CONTABO_CLIENT_SECRET" \
  --data-urlencode "username=$CONTABO_API_USER" --data-urlencode "password=$CONTABO_API_PASSWORD" | jq -r .access_token)

capi() { # METHOD PATH [JSON]
  local a=(-sS -X "$1" -H "Authorization: Bearer $TOKEN"
           -H "x-request-id: $(cat /proc/sys/kernel/random/uuid)" -w '\nHTTP %{http_code}\n')
  [ $# -ge 3 ] && a+=(-H 'Content-Type: application/json' --data "$3")
  curl "${a[@]}" "https://api.contabo.com$2"
}

N="_acme-challenge.probe-cm-1.$E2E_ZONE"
capi POST "/v1/dns/zones/$E2E_ZONE/records" "{\"name\":\"$N\",\"type\":\"TXT\",\"ttl\":60,\"prio\":0,\"data\":\"abc\"}"
for ns in ns1 ns2 ns3; do dig -4 +short +norec "@$ns.contabo.net" "$N" TXT; done
```

Tokens expire after 5 minutes. Fetch a new one when you get HTTP 401.

## Measuring propagation

Do not ask a name server for the name before you create the record: each
server caches "not found" for about 60 seconds, which hides the real
propagation time. Poll each server with `dig +norec` once a second after
the create, and again after the delete.

## After probing

1. Write the finding in `docs/contabo-api.md` with the date (in this repo
   and in external-dns-contabo-webhook).
2. Copy the behavior into `internal/contabo/contabotest`.
3. Add or change a test that depends on it.
4. Delete the probe records: collect their `recordId`s and call
   `capi DELETE "/v1/dns/zones/$E2E_ZONE/records/bulk" '{"recordIds":[1,2,3]}'`.
