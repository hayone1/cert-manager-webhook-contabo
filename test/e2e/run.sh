#!/usr/bin/env bash
# End-to-end test: real cert-manager + this webhook in a real cluster, all
# the way to an issued certificate.
#
# Two modes:
#
#   E2E_MODE=fake (default, used by CI)
#     Runs the fake Contabo API with its DNS server (test/e2e/fakecontabo)
#     and Pebble, Let's Encrypt's test ACME server, inside the cluster.
#     cert-manager is installed (or upgraded) so that it checks DNS through
#     the fake. Needs no Contabo account and no internet DNS.
#     Use a throwaway cluster (kind): cert-manager's DNS settings are changed.
#
#   E2E_MODE=real
#     Uses the real Contabo API, a real zone, real DNS and Let's Encrypt
#     STAGING. cert-manager must already be installed (or set
#     E2E_INSTALL_CERT_MANAGER=true). Every name lives under
#     cm-e2e-<id>.<zone>, so it never touches your other records.
#
# What it checks:
#   1. a certificate for <base> and *.<base> is issued (two TXT values on one name)
#   2. fake mode: a certificate in a delegated child zone, and one from a
#      namespaced Issuer with its own Secret
#   3. every challenge record is removed afterwards, even through a delete
#      outage (fake mode), and an unrelated TXT record on the same name survives
#   4. the webhook logged no unexpected errors
#
# Environment:
#   E2E_MODE                  fake (default) or real
#   E2E_IMAGE                 webhook image already in the cluster (default cert-manager-webhook-contabo:e2e)
#   E2E_FAKE_IMAGE            fake API image already in the cluster (default fake-contabo:e2e)
#   E2E_ZONE                  zone. Fake: default example.com. Real: required, must exist in Contabo.
#   CONTABO_CLIENT_ID, CONTABO_CLIENT_SECRET, CONTABO_API_USER, CONTABO_API_PASSWORD
#                             real mode only
#   E2E_EMAIL                 ACME account email (real mode; default none)
#   E2E_ID                    unique run ID (default: random)
#   E2E_KEEP                  "true" keeps everything for debugging
#   E2E_CERT_MANAGER_VERSION  cert-manager chart version (default v1.21.3)
#   E2E_INSTALL_CERT_MANAGER  "true" installs or upgrades cert-manager. Default: true in fake mode,
#                             false in real mode (then cert-manager must be installed)
#   E2E_TIMEOUT               seconds to wait for each step (default 420 fake, 900 real)
#   E2E_FAKE_PORT             local port for the fake API port-forward (default 18080)
#   KUBECONFIG                cluster to use (the current context is used)
#
# Tools: kubectl, helm, curl, jq. Real mode also uses dig when installed.

set -euo pipefail

MODE=${E2E_MODE:-fake}
case "$MODE" in
  fake)
    # Fixed credentials of the fake API (internal/contabo/contabotest). Not secret.
    CONTABO_CLIENT_ID=INT-test
    CONTABO_CLIENT_SECRET=test-secret
    CONTABO_API_USER=user@example.com
    CONTABO_API_PASSWORD=test-password
    E2E_ZONE=${E2E_ZONE:-example.com}
    INSTALL_CM=${E2E_INSTALL_CERT_MANAGER:-true}
    TIMEOUT=${E2E_TIMEOUT:-420}
    ;;
  real)
    : "${CONTABO_CLIENT_ID:?}" "${CONTABO_CLIENT_SECRET:?}" "${CONTABO_API_USER:?}" "${CONTABO_API_PASSWORD:?}"
    : "${E2E_ZONE:?set E2E_ZONE to a zone that exists in Contabo}"
    INSTALL_CM=${E2E_INSTALL_CERT_MANAGER:-false}
    TIMEOUT=${E2E_TIMEOUT:-900}
    ;;
  *) echo "E2E_MODE must be fake or real" >&2; exit 2 ;;
esac

ZONE=$(echo "$E2E_ZONE" | tr '[:upper:]' '[:lower:]')
ID=${E2E_ID:-$(od -An -N4 -tx4 /dev/urandom | tr -dc '0-9a-f')}
NS="cm-e2e-${ID}"                 # test namespace: fake, Pebble, Certificates
CM_NS=cert-manager                # cert-manager's namespace
RELEASE="cm-e2e-${ID}"            # webhook Helm release (in CM_NS)
GROUP="e2e-${ID}.contabo.acme.codejourney.io"
SECRET="cm-e2e-${ID}-contabo"     # credentials Secret for the ClusterIssuer
ISSUER="cm-e2e-${ID}"
IMAGE=${E2E_IMAGE:-cert-manager-webhook-contabo:e2e}
FAKE_IMAGE=${E2E_FAKE_IMAGE:-fake-contabo:e2e}
PEBBLE_IMAGE=ghcr.io/letsencrypt/pebble:2.10.1@sha256:ddf230642b1a584f519f32e347de1b05a6e4c1f6c35c1863b33effeab5f78199
CM_VERSION=${E2E_CERT_MANAGER_VERSION:-v1.21.3}
HERE=$(cd "$(dirname "$0")" && pwd)
CHART="$HERE/../../charts/cert-manager-webhook-contabo"
PF_PID=""
START=$(date +%s)

if [ "$MODE" = "fake" ]; then
  BASE=$ZONE
  API_URL="http://127.0.0.1:${E2E_FAKE_PORT:-18080}"
  AUTH_URL="$API_URL/token"
  WEBHOOK_API_URL="http://contabo-fake.${NS}.svc.cluster.local:8080"
  WEBHOOK_AUTH_URL="$WEBHOOK_API_URL/token"
else
  BASE="cm-e2e-${ID}.${ZONE}"
  API_URL=https://api.contabo.com
  AUTH_URL=https://auth.contabo.com/auth/realms/contabo/protocol/openid-connect/token
  WEBHOOK_API_URL=""
  WEBHOOK_AUTH_URL=""
fi

log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
pass() { printf '\033[1;32m    ok:\033[0m %s (%ss)\n' "$*" "$(( $(date +%s) - START ))"; }
fail() { printf '\033[1;31m    FAIL:\033[0m %s\n' "$*" >&2; dump; exit 1; }

for tool in kubectl helm curl jq; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

# ---------------------------------------------------------------- Contabo API

TOKEN=""
TOKEN_AT=0
token() {
  local now; now=$(date +%s)
  if [ -z "$TOKEN" ] || [ $((now - TOKEN_AT)) -gt 200 ]; then
    TOKEN=$(curl -sSf --retry 4 --retry-delay 3 --retry-all-errors "$AUTH_URL" \
      --data-urlencode grant_type=password \
      --data-urlencode "client_id=$CONTABO_CLIENT_ID" \
      --data-urlencode "client_secret=$CONTABO_CLIENT_SECRET" \
      --data-urlencode "username=$CONTABO_API_USER" \
      --data-urlencode "password=$CONTABO_API_PASSWORD" | jq -r .access_token)
    TOKEN_AT=$now
  fi
  printf '%s' "$TOKEN"
}

api() { # METHOD PATH [BODY]
  # Retry on errors: the Contabo API has short outages, and a failed read
  # must never look like "no records".
  local args=(-sSf --retry 4 --retry-delay 3 --retry-all-errors -X "$1"
              -H "Authorization: Bearer $(token)" -H "x-request-id: $(cat /proc/sys/kernel/random/uuid)")
  [ $# -ge 3 ] && args+=(-H 'Content-Type: application/json' --data "$3")
  curl "${args[@]}" "$API_URL$2"
}

# zone_of NAME: the zone that holds NAME in this test.
zone_of() { if [ "$MODE" = fake ] && [[ "$1" == *".sub.${ZONE}" || "$1" == "sub.${ZONE}" ]]; then echo "sub.${ZONE}"; else echo "$ZONE"; fi; }

# challenge_records ZONE: "name data id" of every TXT record under
# _acme-challenge names that belong to this run.
challenge_records() {
  api GET "/v1/dns/zones/$1/records?size=1000" | jq -r --arg z "$1" --arg b "$BASE" '
    def fqdn: (.name // "") | ascii_downcase | rtrimstr(".")
      | if . == "" or . == $z then $z elif endswith("." + $z) then . else . + "." + $z end;
    .data[] | select(.type == "TXT") | (fqdn) as $n
    | select($n | startswith("_acme-challenge."))
    | select(($n | endswith("." + $b)) or ($n == "_acme-challenge." + $b))
    | "\($n) \(.data) \(.recordId)"'
}

# acme_records ZONE: challenge records except the one we planted on purpose.
acme_records() { challenge_records "$1" | grep -v ' keep-me ' || true; }

# wait_until DESCRIPTION COMMAND...: retry COMMAND until it succeeds.
wait_until() {
  local what=$1; shift
  local deadline=$(( $(date +%s) + TIMEOUT ))
  until "$@" >/dev/null 2>&1; do
    [ "$(date +%s)" -lt "$deadline" ] || fail "timed out waiting for: $what"
    sleep 5
  done
  pass "$what"
}

no_acme_records() {
  local z out
  for z in "$@"; do
    out=$(acme_records "$z") || return 1
    [ -z "$out" ] || return 1
  done
}

# ------------------------------------------------------------------- cluster

webhook_logs() { kubectl -n "$CM_NS" logs -l "app.kubernetes.io/instance=${RELEASE}" --tail=-1 --prefix 2>/dev/null || true; }

dump() {
  echo "---- challenge TXT records ----"; challenge_records "$ZONE" || true
  echo "---- certificates, orders, challenges ----"
  kubectl -n "$NS" get certificate,certificaterequest,order,challenge -o wide 2>/dev/null || true
  kubectl -n "$NS" describe challenge 2>/dev/null | grep -E "Name:|Reason|State|Presented|Message" | head -40 || true
  echo "---- pods ----"; kubectl -n "$NS" get pods -o wide || true; kubectl -n "$CM_NS" get pods -o wide || true
  echo "---- webhook (last 60 lines) ----"; webhook_logs | tail -60
  echo "---- cert-manager (last 40 lines) ----"; kubectl -n "$CM_NS" logs deploy/cert-manager --tail=40 2>/dev/null || true
  if [ "$MODE" = "fake" ]; then
    echo "---- fake Contabo API (last 30 lines) ----"; kubectl -n "$NS" logs deploy/contabo-fake --tail=30 || true
    echo "---- Pebble (last 30 lines) ----"; kubectl -n "$NS" logs deploy/pebble --tail=30 || true
  fi
}

cleanup() {
  local code=$?
  if [ "${E2E_KEEP:-false}" = "true" ]; then
    log "E2E_KEEP=true, leaving namespace $NS, release $RELEASE and records in place"
    exit "$code"
  fi
  log "cleaning up"
  kubectl -n "$NS" delete certificate --all --wait=true --timeout=60s >/dev/null 2>&1 || true
  if [ "$MODE" = "real" ]; then
    # Give cert-manager a moment to clean up, then remove anything left
    # under this run's names directly.
    local ids; ids=$(challenge_records "$ZONE" 2>/dev/null | awk '{print $NF}' | paste -sd, - || true)
    if [ -n "$ids" ]; then
      echo "    deleting leftover challenge record IDs: $ids"
      api DELETE "/v1/dns/zones/${ZONE}/records/bulk" "{\"recordIds\":[${ids}]}" >/dev/null || true
    fi
  fi
  kubectl delete clusterissuer "$ISSUER" --ignore-not-found >/dev/null 2>&1 || true
  helm uninstall "$RELEASE" --namespace "$CM_NS" --wait >/dev/null 2>&1 || true
  kubectl -n "$CM_NS" delete secret "$SECRET" --ignore-not-found >/dev/null 2>&1 || true
  [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null || true
  kubectl delete namespace "$NS" --wait=false --ignore-not-found >/dev/null 2>&1 || true
  exit "$code"
}
trap cleanup EXIT

start_fake() {
  kubectl -n "$NS" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: contabo-fake
spec:
  replicas: 1
  selector: { matchLabels: { app: contabo-fake } }
  template:
    metadata:
      labels: { app: contabo-fake }
    spec:
      securityContext: { runAsNonRoot: true, runAsUser: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: fake
          image: "${FAKE_IMAGE}"
          imagePullPolicy: IfNotPresent
          args: ["-zones", "${ZONE},sub.${ZONE}", "-dns-addr", ":5353"]
          ports:
            - { name: http, containerPort: 8080 }
            - { name: dns-udp, containerPort: 5353, protocol: UDP }
            - { name: dns-tcp, containerPort: 5353, protocol: TCP }
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: { drop: ["ALL"] }
---
apiVersion: v1
kind: Service
metadata:
  name: contabo-fake
spec:
  selector: { app: contabo-fake }
  ports:
    - { name: http, port: 8080, targetPort: 8080 }
    - { name: dns-udp, port: 53, targetPort: 5353, protocol: UDP }
    - { name: dns-tcp, port: 53, targetPort: 5353, protocol: TCP }
EOF
  kubectl -n "$NS" rollout status deploy/contabo-fake --timeout=2m >/dev/null
  FAKE_DNS="$(kubectl -n "$NS" get svc contabo-fake -o jsonpath='{.spec.clusterIP}'):53"
  kubectl -n "$NS" port-forward svc/contabo-fake "${E2E_FAKE_PORT:-18080}:8080" >/dev/null 2>&1 &
  PF_PID=$!
  local i
  for i in $(seq 1 30); do
    curl -sf "$API_URL/healthz" >/dev/null && { pass "fake Contabo API is running, DNS at ${FAKE_DNS}"; return; }
    sleep 1
  done
  fail "fake Contabo API did not answer on $API_URL"
}

# fake_outage COUNT: the next COUNT record deletes fail with HTTP 500, like
# the real outage seen on 2026-11-03.
fake_outage() {
  curl -sSf -X POST "$API_URL/_fake/faults" -H 'Content-Type: application/json' \
    --data "{\"method\":\"DELETE\",\"path\":\"/records/\",\"status\":500,\"count\":$1}" >/dev/null
}

install_cert_manager() {
  local args=(--namespace "$CM_NS" --create-namespace --version "$CM_VERSION" --wait --timeout 5m
              --set crds.enabled=true)
  if [ "$MODE" = "fake" ]; then
    # Check DNS only through the fake name server; there is no real DNS for example.com.
    args+=(--set 'extraArgs={--dns01-recursive-nameservers-only,--dns01-recursive-nameservers='"$FAKE_DNS"'}')
  fi
  helm repo add jetstack https://charts.jetstack.io --force-update >/dev/null
  helm upgrade --install cert-manager jetstack/cert-manager "${args[@]}" >/dev/null
  pass "cert-manager ${CM_VERSION} is running"
}

start_pebble() {
  # Pebble's HTTPS certificate comes from cert-manager: a throwaway CA in
  # the test namespace. The ClusterIssuer trusts that CA through caBundle.
  kubectl -n "$NS" apply -f - >/dev/null <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata: { name: selfsigned }
spec: { selfSigned: {} }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: { name: pebble-ca }
spec:
  isCA: true
  commonName: pebble-e2e-ca
  secretName: pebble-ca
  issuerRef: { name: selfsigned }
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata: { name: pebble-ca }
spec: { ca: { secretName: pebble-ca } }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: { name: pebble-tls }
spec:
  secretName: pebble-tls
  issuerRef: { name: pebble-ca }
  dnsNames: ["pebble.${NS}.svc", "pebble.${NS}.svc.cluster.local"]
---
apiVersion: v1
kind: ConfigMap
metadata: { name: pebble }
data:
  pebble-config.json: |
    {"pebble": {"listenAddress": "0.0.0.0:14000", "managementListenAddress": "0.0.0.0:15000",
      "certificate": "/tls/tls.crt", "privateKey": "/tls/tls.key",
      "httpPort": 5002, "tlsPort": 5001, "ocspResponderURL": "", "externalAccountBindingRequired": false}}
EOF
  kubectl -n "$NS" wait --for=condition=Ready certificate/pebble-tls --timeout=2m >/dev/null
  kubectl -n "$NS" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: { name: pebble }
spec:
  replicas: 1
  selector: { matchLabels: { app: pebble } }
  template:
    metadata: { labels: { app: pebble } }
    spec:
      securityContext: { runAsNonRoot: true, runAsUser: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: pebble
          image: ${PEBBLE_IMAGE}
          args: ["-config", "/config/pebble-config.json", "-dnsserver", "${FAKE_DNS}"]
          env:
            - { name: PEBBLE_VA_NOSLEEP, value: "1" }
            - { name: PEBBLE_WFE_NONCEREJECT, value: "0" }
            - { name: PEBBLE_AUTHZREUSE, value: "0" }
          ports: [{ containerPort: 14000 }]
          readinessProbe: { tcpSocket: { port: 14000 } }
          volumeMounts:
            - { name: config, mountPath: /config }
            - { name: tls, mountPath: /tls }
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: { drop: ["ALL"] }
      volumes:
        - { name: config, configMap: { name: pebble } }
        - { name: tls, secret: { secretName: pebble-tls } }
---
apiVersion: v1
kind: Service
metadata: { name: pebble }
spec:
  selector: { app: pebble }
  ports: [{ port: 14000, targetPort: 14000 }]
EOF
  kubectl -n "$NS" rollout status deploy/pebble --timeout=2m >/dev/null
  pass "Pebble is running"
}

credentials_secret() { # NAMESPACE NAME
  jq -n --arg ns "$1" --arg name "$2" --arg id "$CONTABO_CLIENT_ID" --arg secret "$CONTABO_CLIENT_SECRET" \
        --arg user "$CONTABO_API_USER" --arg pw "$CONTABO_API_PASSWORD" '{
    apiVersion: "v1", kind: "Secret", type: "Opaque",
    metadata: { name: $name, namespace: $ns },
    stringData: { "client-id": $id, "client-secret": $secret, "api-user": $user, "api-password": $pw }
  }' | kubectl apply -f - >/dev/null
}

issuer() { # KIND NAME NAMESPACE SECRET
  local server ca="" meta
  if [ "$MODE" = "fake" ]; then
    server="https://pebble.${NS}.svc:14000/dir"
    ca=$(kubectl -n "$NS" get secret pebble-tls -o jsonpath='{.data.ca\.crt}')
  else
    server=https://acme-staging-v02.api.letsencrypt.org/directory
  fi
  meta="{ name: $2 }"
  [ "$1" = Issuer ] && meta="{ name: $2, namespace: $3 }"
  kubectl apply -f - >/dev/null <<EOF
apiVersion: cert-manager.io/v1
kind: $1
metadata: $meta
spec:
  acme:
    server: ${server}
    ${ca:+caBundle: ${ca}}
    ${E2E_EMAIL:+email: ${E2E_EMAIL}}
    privateKeySecretRef: { name: $2-account }
    solvers:
      - dns01:
          webhook:
            groupName: ${GROUP}
            solverName: contabo
            config:
              secretName: $4
              ttl: 60
EOF
  kubectl wait --for=condition=Ready "$(echo "$1" | tr '[:upper:]' '[:lower:]')/$2" ${3:+-n "$3"} --timeout=2m >/dev/null \
    || fail "$1 $2 did not become ready"
}

certificate() { # NAME ISSUER_KIND ISSUER DNSNAME...
  local name=$1 kind=$2 iss=$3; shift 3
  local names; names=$(printf '"%s",' "$@"); names="[${names%,}]"
  kubectl -n "$NS" apply -f - >/dev/null <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: { name: ${name} }
spec:
  secretName: ${name}-tls
  dnsNames: ${names}
  issuerRef: { kind: ${kind}, name: ${iss} }
  privateKey: { algorithm: ECDSA, size: 256 }
EOF
}

cert_ready() { [ "$(kubectl -n "$NS" get certificate "$1" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')" = "True" ]; }

# wait_cert NAME: wait until the certificate is ready, but stop at once when
# the ACME server rejects a challenge: cert-manager would only retry after
# an hour, so waiting longer cannot help.
wait_cert() {
  local deadline=$(( $(date +%s) + TIMEOUT )) bad
  until cert_ready "$1"; do
    bad=$(kubectl -n "$NS" get challenge -o json 2>/dev/null | jq -r --arg c "$1" '
      .items[] | select(.metadata.name | startswith($c + "-")) | select(.status.state == "invalid")
      | "\(.spec.dnsName): \(.status.reason)"' | head -3)
    if [ -n "$bad" ]; then
      if grep -q "query timed out" <<<"$bad"; then
        echo "    hint: the ACME server could not reach the zone's name servers at all."
        echo "    Check https://letsdebug.net (method dns-01) and docs/edge-cases.md."
      fi
      fail "the ACME server rejected a challenge for certificate $1: $bad"
    fi
    [ "$(date +%s)" -lt "$deadline" ] || fail "timed out waiting for certificate $1"
    sleep 5
  done
  pass "certificate $1 is ready"
}

# check_dns_absent: real mode only. Succeeds once no Contabo name server
# serves a challenge value at _acme-challenge.<base> any more. The planted
# "keep-me" record lives on the same name and is removed later, so it is
# ignored here.
check_dns_absent() {
  [ "$MODE" = real ] && command -v dig >/dev/null || return 0
  local ns
  for ns in ns1.contabo.net ns2.contabo.net ns3.contabo.net; do
    if dig -4 +short +norec +time=3 +tries=2 "@$ns" "_acme-challenge.$BASE" TXT 2>/dev/null | grep -v '^"keep-me"$' | grep -q .; then
      return 1
    fi
  done
}

# ---------------------------------------------------------------------- run

log "run ${ID} (${MODE} mode): zone ${ZONE}, names under ${BASE}, image ${IMAGE}, groupName ${GROUP}"
kubectl create namespace "$NS" >/dev/null
FAKE_DNS=""
if [ "$MODE" = "fake" ]; then
  start_fake
fi
api GET "/v1/dns/zones/${ZONE}" >/dev/null || { echo "zone ${ZONE} not found" >&2; exit 2; }
[ -z "$(acme_records "$ZONE")" ] || { echo "challenge records already exist under ${BASE}" >&2; exit 2; }

if [ "$INSTALL_CM" = "true" ]; then
  log "installing cert-manager ${CM_VERSION}"
  install_cert_manager
else
  kubectl -n "$CM_NS" rollout status deploy/cert-manager --timeout=30s >/dev/null \
    || { echo "cert-manager is not installed in $CM_NS; set E2E_INSTALL_CERT_MANAGER=true" >&2; exit 2; }
  pass "cert-manager is already running"
fi

log "installing the webhook"
credentials_secret "$CM_NS" "$SECRET"
helm_args=(--namespace "$CM_NS" --wait --timeout 3m
  --set "groupName=${GROUP}"
  --set "image.repository=${IMAGE%:*}" --set "image.tag=${IMAGE##*:}" --set image.pullPolicy=IfNotPresent
  --set "secretNames[0]=${SECRET}"
  --set logLevel=4)
if [ "$MODE" = "fake" ]; then
  credentials_secret "$NS" contabo-credentials
  helm_args+=(--set "contabo.apiURL=${WEBHOOK_API_URL}" --set "contabo.authURL=${WEBHOOK_AUTH_URL}"
              --set "extraSecretAccess[0].namespace=${NS}" --set "extraSecretAccess[0].secretNames[0]=contabo-credentials")
fi
helm upgrade --install "$RELEASE" "$CHART" "${helm_args[@]}" >/dev/null
wait_until "APIService v1alpha1.${GROUP} is available" \
  sh -c "[ \"\$(kubectl get apiservice v1alpha1.${GROUP} -o jsonpath='{.status.conditions[?(@.type==\"Available\")].status}')\" = True ]"

if [ "$MODE" = "fake" ]; then
  start_pebble
fi
issuer ClusterIssuer "$ISSUER" "" "$SECRET"
pass "ClusterIssuer $ISSUER is ready"

# A TXT record on the challenge name that is not ours. It must survive.
ttl='"ttl":60,"prio":0'
api POST "/v1/dns/zones/${ZONE}/records" "{\"name\":\"_acme-challenge.${BASE}\",\"type\":\"TXT\",${ttl},\"data\":\"keep-me\"}" >/dev/null
pass "planted an unrelated TXT record at _acme-challenge.${BASE}"

if [ "$MODE" = "fake" ]; then
  # Enough failures to outlast the webhook's own retries (5 attempts per
  # record, several challenges at once), so cert-manager has to call
  # CleanUp again later.
  fake_outage 25
  pass "simulating a Contabo outage: the next 25 record deletes fail with HTTP 500"
fi

log "step 1: certificate for ${BASE} and *.${BASE}"
certificate apex-wild ClusterIssuer "$ISSUER" "$BASE" "*.${BASE}"
if [ "$MODE" = "fake" ]; then
  log "step 2: delegated child zone, and a namespaced Issuer"
  certificate child ClusterIssuer "$ISSUER" "app.sub.${ZONE}"
  issuer Issuer team-issuer "$NS" contabo-credentials
  certificate namespaced Issuer team-issuer "team.${ZONE}"
fi

wait_cert apex-wild
ISSUED=$(( $(date +%s) - START ))
dns=$(kubectl -n "$NS" get secret apex-wild-tls -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -ext subjectAltName 2>/dev/null || true)
[ -z "$dns" ] || grep -q "\*\.${BASE}" <<<"$dns" || fail "issued certificate lacks *.${BASE}: $dns"
if [ "$MODE" = "fake" ]; then
  wait_cert child         # zone sub.${ZONE}
  wait_cert namespaced    # namespaced Issuer with its own Secret
fi

log "step 3: cleanup"
zones=("$ZONE"); [ "$MODE" = "fake" ] && zones+=("sub.${ZONE}")
wait_until "every challenge record is gone" no_acme_records "${zones[@]}"
challenge_records "$ZONE" | grep -q ' keep-me ' || fail "the unrelated TXT record was deleted"
pass "the unrelated TXT record is still there"
if [ "$MODE" = "fake" ]; then
  webhook_logs | grep -q "could not clean up the challenge record" || fail "the delete outage was never hit"
  pass "cleanup recovered from the delete outage"
else
  wait_until "Contabo name servers no longer serve the challenge" check_dns_absent
fi
id=$(challenge_records "$ZONE" | awk '/ keep-me /{print $NF}')
[ -n "$id" ] && api DELETE "/v1/dns/zones/${ZONE}/records/${id}" >/dev/null

# Errors caused by Contabo itself (HTTP 5xx, 429) are expected now and then
# and are retried. Any other error means a bug.
transient='HTTP (5[0-9][0-9]|429)|could not clean up the challenge record|did not finish'
all_errors=$(webhook_logs | grep -E '"level"="error"|level=ERROR| E[0-9]{4} ' || true)
real_errors=$(grep -vE "$transient" <<<"$all_errors" | sed '/^$/d' || true)
if [ -n "$real_errors" ]; then
  echo "$real_errors" | tail -20
  fail "webhook logged $(wc -l <<<"$real_errors") unexpected errors"
fi
pass "webhook logged no unexpected errors"

log "end-to-end test passed: first certificate after ${ISSUED}s, total $(( $(date +%s) - START ))s"
