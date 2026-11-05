---
name: release
description: Cut a new release of the webhook (tag, publish signed multi-arch images to GHCR and Docker Hub and the signed Helm chart to GHCR) and check the result. Use when asked to release, publish an image or chart, or bump the version.
---

# Cut a release

Releases are automated by `.github/workflows/release.yml`. A human picks the
version and pushes a tag. Always confirm the version with the user before
pushing a tag: a pushed tag publishes images and a chart.

## Steps

1. `main` must be green: `gh run list --branch main --limit 5`.
2. Run the real-API tests locally (see the `run-e2e` and `run-conformance`
   skills). CI only uses the fake API.
3. Pick the version (semver; on v0.x bump the minor for breaking changes).
4. In a PR: set `version` and `appVersion` in
   `charts/cert-manager-webhook-contabo/Chart.yaml` (no `v`) and update
   `CHANGELOG.md`. Merge it.
5. Tag the merge commit and push:
   `git tag -a vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`.
6. Watch:
   `gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId -q '.[0].databaseId')"`.

## Check the result

Follow `docs/releasing.md#check-a-release`: anonymous pull, `cosign verify`
(cosign 3 or newer) for both image registries and the chart,
`gh attestation verify`, and `helm install` from the OCI chart.

## If something fails

See `docs/releasing.md#if-something-fails`. Never move a tag that already
published images; release a new patch instead.
