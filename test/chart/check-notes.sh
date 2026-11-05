#!/usr/bin/env bash
# Checks that the chart's install notes (templates/NOTES.txt) match the
# credentials setup. "helm template" does not render NOTES.txt, and
# "helm install --dry-run" needs a cluster, so this script renders the notes
# through a temporary copy of the chart.
set -euo pipefail

chart=${1:?usage: check-notes.sh <chart dir>}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cp -r "$chart" "$tmp/chart"
cp "$tmp/chart/templates/NOTES.txt" "$tmp/chart/notes.txt"
cat >"$tmp/chart/templates/zz-notes-check.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: notes-check
data:
  notes: |
{{- tpl (.Files.Get "notes.txt") . | nindent 4 }}
EOF

render() {
  helm template notes-check "$tmp/chart" -n cert-manager \
    --show-only templates/zz-notes-check.yaml "$@"
}

fail() { echo "NOTES.txt: $*" >&2; exit 1; }

notes=$(render)
grep -q 'create secret generic contabo-credentials' <<<"$notes" || fail "default: Secret command missing"
grep -q 'secretName: contabo-credentials' <<<"$notes" || fail "default: issuer must set secretName"

notes=$(render --set ambientCredentials.secretName=contabo-ambient)
grep -q 'create secret generic contabo-ambient' <<<"$notes" || fail "ambient: Secret command missing"
! grep -q 'secretName:' <<<"$notes" || fail "ambient: issuer must not set secretName"

notes=$(render --set ambientCredentials.secretName=contabo-ambient --set ambientCredentials.keys.apiUser=user)
grep -q -- "--from-literal=user=" <<<"$notes" || fail "ambient: custom key names not used"

echo "install notes ok"
