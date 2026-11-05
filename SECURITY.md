# Security policy

## Supported versions

Only the newest release gets security fixes. Please upgrade before you
report a problem.

## Reporting a problem

**Do not open a public issue for a security problem.**

Use GitHub's private reporting instead:

1. Go to the [Security tab](https://github.com/hayone1/cert-manager-webhook-contabo/security).
2. Click **Report a vulnerability**.
3. Tell us what you found, how to reproduce it, and what an attacker could do with it.

We will answer within 7 days. When the fix is out, we will credit you in the
release notes unless you ask us not to.

## What counts

Good examples:

- the webhook leaks Contabo credentials or access tokens (in logs, errors or challenge status)
- the webhook changes or deletes DNS records other than the challenge record it made
- a namespaced Issuer can make the webhook use credentials from another namespace
- someone other than cert-manager can call the webhook
- a published image or chart contains a known, fixable, high or critical vulnerability
- a weakness in the GitHub workflows that could let someone publish a bad image or chart

Not security problems (open a normal issue instead):

- Contabo API outages or rate limits
- slow DNS updates on Contabo's name servers

## How the webhook protects your account

- It reads a Secret only from the challenge's own namespace (the Issuer's
  namespace, or cert-manager's namespace for a ClusterIssuer). An issuer
  cannot point it at a Secret in another namespace.
- The Helm chart gives it `get` on the Secret names you list, not on all Secrets.
- Issuers cannot change the Contabo API address. Only the cluster admin can,
  through the webhook's environment.
- Only cert-manager's ServiceAccount may call the webhook.
- It never creates, changes or deletes zones, and only deletes TXT records
  whose value is the challenge key it was given.

## How releases are protected

- Images and charts are built by GitHub Actions only, never on a laptop.
- Every image is signed with [cosign](https://github.com/sigstore/cosign)
  (keyless) and has a signed build provenance and SBOM. The Helm chart is
  signed too.
- Every image is scanned with [Trivy](https://github.com/aquasecurity/trivy).
- The image has no shell, runs as a non-root user and works with a read-only file system.
- All GitHub Actions are pinned to a commit SHA and updated by Dependabot.
- CI never uses a real Contabo account.

You can check that an image really came from this repository (needs cosign 3 or newer):

```sh
cosign verify ghcr.io/hayone1/cert-manager-webhook-contabo:0.1.1 \
  --certificate-identity-regexp '^https://github.com/hayone1/cert-manager-webhook-contabo/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
