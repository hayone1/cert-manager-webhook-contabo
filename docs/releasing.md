# Releasing

Releases are automated by [`.github/workflows/release.yml`](../.github/workflows/release.yml).
A maintainer picks the version and pushes a tag. Everything else happens in
GitHub Actions, never on a laptop.

## What the release workflow does

1. Checks that the tag is on `main` and matches `version` and `appVersion`
   in `charts/cert-manager-webhook-contabo/Chart.yaml`.
2. Runs the unit tests and the end-to-end test (fake Contabo API).
3. Builds the image for `linux/amd64` and `linux/arm64`, with an SBOM and
   build provenance, and pushes it to GHCR.
4. Signs the image with cosign (keyless) and attests its provenance.
5. Copies the same image (same digest) to Docker Hub, and signs it there too.
6. Packages the Helm chart, pushes it to `oci://ghcr.io/hayone1/charts`, and
   signs it.
7. Scans the image with Trivy and uploads the result to GitHub code scanning.
8. Creates the GitHub release with the image and chart digests.
9. Updates the Docker Hub page from [dockerhub.md](dockerhub.md), if the
   `DOCKERHUB_DESCRIPTION_TOKEN` secret is set.

## Cut a release

1. Make sure `main` is green.
2. Run the tests against the real Contabo API on your machine
   ([development.md](development.md#tests-against-the-real-contabo-api)).
   CI cannot catch changes on Contabo's side.
3. Pick the version with [semver](https://semver.org/). While on `v0.x`,
   bump the minor number for breaking changes.
4. In a pull request:
   - set `version` and `appVersion` in `charts/cert-manager-webhook-contabo/Chart.yaml`
     to the new version (without `v`),
   - update [CHANGELOG.md](../CHANGELOG.md).
5. Merge it, then tag the merge commit:

   ```sh
   git tag -a v0.1.0 -m v0.1.0
   git push origin v0.1.0
   ```

6. Watch the run:
   `gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId -q '.[0].databaseId')"`

Never move a tag that already published images. Release a new patch instead.

## Image tags

| Tag | Example | Moves? |
|---|---|---|
| exact version | `0.1.0` and `v0.1.0` | never |
| minor | `0.1` | yes, to the newest patch |
| major (from v1 on) | `1` | yes |
| `latest` | | yes, newest non-pre-release |
| commit | `sha-<40 hex>` | never |

## Check a release

Needs [cosign](https://github.com/sigstore/cosign) **3 or newer** (cosign 2
says "no signatures found").

```sh
VERSION=0.1.0
ID='^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/'
ISSUER=https://token.actions.githubusercontent.com

# Image (both registries hold the same digest)
cosign verify ghcr.io/hayone1/cert-manager-webhook-contabo:$VERSION \
  --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER"
cosign verify docker.io/hayone/cert-manager-webhook-contabo:$VERSION \
  --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER"

# Build provenance
gh attestation verify oci://ghcr.io/hayone1/cert-manager-webhook-contabo:$VERSION \
  --repo hayone1/cert-manager-webhook-contabo

# Helm chart
cosign verify ghcr.io/hayone1/charts/cert-manager-webhook-contabo:$VERSION \
  --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER"

# Install from the chart
helm install cert-manager-webhook-contabo \
  oci://ghcr.io/hayone1/charts/cert-manager-webhook-contabo --version $VERSION \
  --namespace cert-manager
```

## If something fails

- **Tests or e2e failed:** nothing was published. Fix it on `main`, delete
  the tag (`git push --delete origin vX.Y.Z && git tag -d vX.Y.Z`), tag again.
- **"Chart.yaml version does not match tag":** update `Chart.yaml` in a PR first.
- **Docker Hub push failed:** check `DOCKER_USERNAME` and `DOCKER_PASSWORD`
  in the `release` environment. The token needs Read & Write scope. Re-run
  the failed job.
- **"Docker Hub description" failed with `Forbidden`:** `DOCKERHUB_DESCRIPTION_TOKEN`
  needs Read, Write & Delete scope.
- **Chart push failed:** the `GITHUB_TOKEN` needs `packages: write`, and the
  `charts/cert-manager-webhook-contabo` package on GHCR must allow this repository
  to write (Package settings, "Manage Actions access").

## Repository settings (one time)

- Ruleset on `main`: pull requests required, no force push, no deletion,
  required checks `CI passed` and `kind + cert-manager + Pebble + fake Contabo`.
- Environment `release`: only `v*` tags may deploy; secrets `DOCKER_USERNAME`
  and `DOCKER_PASSWORD` (optional `DOCKERHUB_DESCRIPTION_TOKEN`).
- Secret scanning, push protection, private vulnerability reporting and
  Dependabot alerts on.
- No Contabo secrets anywhere in GitHub.
