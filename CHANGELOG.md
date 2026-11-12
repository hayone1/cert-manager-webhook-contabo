# Changelog

All notable changes are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [semantic versioning](https://semver.org/).

## [Unreleased]

## [0.1.2] - 2026-11-12

### Security

- Built with Go 1.27.4. Go 1.27.3 had GO-2026-6859, a bug that can crash an
  HTTP/1 server.
- Updated the etcd client modules to v3.7.1 (GO-2026-6107) and
  `golang.org/x/crypto` to v0.59.0 (GO-2026-5932).

### Changed

- The client cache no longer stores a SHA-256 hash of the Contabo
  credentials. It keys each cached client by the credentials themselves,
  which stay in memory only. A changed password still gets a new client at
  once.
- Updated `github.com/miekg/dns`.

## [0.1.1] - 2026-11-05

### Fixed

- Chart: when `ambientCredentials.secretName` is set, the install notes now
  show how to create the ambient credentials Secret (with its key names) and
  a ClusterIssuer without `secretName`. Before, they always showed the
  `contabo-credentials` Secret. `make chart-lint` now checks the notes.

### Changed

- Plainer README, chart README and docs. `docs/configuration.md` has full
  examples of every Secret format.

## [0.1.0] - 2026-11-05

### Added

- cert-manager DNS-01 webhook for Contabo DNS (`solverName: contabo`,
  default `groupName: contabo.acme.codejourney.io`).
- Issuer settings `secretName`, `secretKeys`, `ttl` and `zone`; ambient
  credentials for ClusterIssuers.
- Safe handling of several TXT values on one name, every Contabo name
  spelling, quoted and split TXT values, duplicate creates (409), missing
  records, delete outages, rate limits and expired tokens.
- Zone choice that follows cert-manager's resolved zone, prefers sub-zones,
  and explains when DNS and the Contabo account disagree.
- One cached Contabo client per account, renewed when the Secret changes.
- Helm chart with a self-made serving certificate, least-privilege RBAC
  (Secrets by name only), hardened pod settings, optional PodDisruptionBudget
  and a values schema.
- Fake Contabo API with a DNS server; cert-manager's conformance suite and a
  kind end-to-end test (cert-manager, Pebble, delete outage) in CI, with no
  Contabo account.
- Real-mode tests against the Contabo API and Let's Encrypt staging for
  maintainers.
- Signed multi-arch images on GHCR and Docker Hub, signed OCI Helm chart.

[Unreleased]: https://github.com/hayone1/cert-manager-webhook-contabo/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/hayone1/cert-manager-webhook-contabo/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/hayone1/cert-manager-webhook-contabo/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/hayone1/cert-manager-webhook-contabo/releases/tag/v0.1.0
