# Changelog

All notable changes to this project are documented here. Versions follow
[Semantic Versioning](https://semver.org).

## Unreleased

- `--redact` masks OpenShift values: the `<cluster>.<base domain>` part of API hosts
  (`api.redacted:6443`, `api-int.redacted`) and of every host under `apps.<cluster>.<base>` (Route,
  Ingress and HTTPRoute hosts, error messages), the dashed API host in `oc login` context, cluster
  and user names (`ns-a/api-redacted:6443/user-a`), and team names taken from the
  `openshift.io/requester` annotation, which are user names (`redacted-1`, `redacted-2`, the same
  mask for the same user across the snapshots and the comparison of one run). API groups and CRD
  names such as `apps.openshift.io` keep their names. Snapshots record where each namespace team
  came from (`namespaces[].team_from`); collect again so `--redact` can find requester teams.
- Ring scales for long-lived clusters: `--rings` takes the suffix `y` (365 days), e.g.
  `--rings 30d,90d,180d,1y,2y,3y,5y`; the preset `years` (30d, 90d, 180d, 1y … 10y) in
  `render`/`compare --scale` and in the page; and `auto`, with rings at quantiles of the resource
  ages of the rendered snapshots, rounded to readable values (`270d,1y,1.5y,2y,2.5y,3y`), computed
  for both axes. The default stays `30d`: on the kuberito lab, `auto` squeezes the migration waves of
  the target into the first rings of the comparison. Ring labels show half years as `1.5y`.
- Argo CD attribution from `status.resources` of the Applications that deploy to the collected
  cluster, whatever tracking method the instance uses (the label and annotation checks stay). An
  Application with another destination is counted as remote and not used. Snapshots carry
  `cluster.argocd` (`applications`, `remote`, `managed`, `from_status`) when the cluster serves the
  Application kind; the page says "Argo CD installed, no managed objects found" when nothing is
  attributed.
- `collect --skip-kinds Kind,group/Kind` leaves kinds out (recorded as `scope.skipped_kinds`), and
  `collect --no-secrets` runs the inventory with a role that cannot list Secrets. Without Secrets,
  the Helm release state and the links to Secrets are lost; Helm is still detected from object
  labels, and references to skipped kinds are not reported as missing.
- Published RBAC: [`deploy/rbac/kubestrata-reader.yaml`](deploy/rbac/kubestrata-reader.yaml)
  (`list` on every kind) and
  [`deploy/rbac/kubestrata-reader-no-secrets.yaml`](deploy/rbac/kubestrata-reader-no-secrets.yaml),
  each with a ServiceAccount and a ClusterRoleBinding; README "Permissions" shows how to collect
  with a short-lived ServiceAccount token. They replace the `inventory-reader` role the design
  mentioned but never shipped.

## 0.1.4 — 2026-11-27

- PyPI: `uvx kubestrata`, `uv tool install kubestrata`, `pipx install kubestrata`. One wheel per
  platform (macOS 12+ and Linux glibc/musl, arm64/amd64) with the binary of the signed release
  archive in `kubestrata-<version>.data/scripts/`, built by `scripts/build-wheels.py` (standard
  library, reproducible) after verifying the cosign signature and the checksums. Published by
  Trusted Publishing from the `pypi` environment (no stored token), with PEP 740 attestations;
  pre-release tags (`vX.Y.Z-rc.N`) go to TestPyPI only and are marked as pre-release on GitHub.
- `install-smoke.yml` installs with `uv tool install` too (16 jobs).
- Docs: [docs/security-analysis.md](docs/security-analysis.md) (EN/PT-BR) lists the security
  checks, the findings of 0.1.2 and 0.1.3 and how to repeat the checks on a release; README
  Security section and badge; SECURITY.md with supported versions.

## 0.1.3 — 2026-11-26

Security:

- Built with Go 1.27.4 (was 1.27.1). `govulncheck` found 11 vulnerabilities of the standard
  library reachable from kubestrata in 0.1.0–0.1.2, in `net/http`, the HTTP/2 client,
  `crypto/tls`, `crypto/x509` and `net/textproto` (GO-2026-6859, GO-2026-6809, GO-2026-6617,
  GO-2026-6613, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6608, GO-2026-6607,
  GO-2026-6605, GO-2026-6603), fixed in Go 1.27.2 to 1.27.4. Upgrade from 0.1.2 or earlier.
- `golang.org/x/net` v0.62.0 (was v0.57.0), with `x/sync`, `x/sys`, `x/term` and `x/text` updated
  by `go mod tidy`. `govulncheck -show verbose` now reports no vulnerability, reachable or not.
- `security.yml`: `govulncheck` on every change and weekly, failing when the code reaches a known
  vulnerability; `zizmor` audit of the workflows. Dependabot for Go modules and GitHub Actions.
- `install-smoke.yml` starts from a `workflow_dispatch` sent by the last job of `release.yml`
  instead of `workflow_run`, and passes values through step outputs instead of `GITHUB_ENV`
  (zizmor `dangerous-triggers` and `github-env`).

## 0.1.2 — 2026-11-26

- Homebrew: the formula moved to the project's own tap, `brew install kubestrata/tap/kubestrata`
  ([kubestrata/homebrew-tap](https://github.com/kubestrata/homebrew-tap)). 0.1.0 and 0.1.1 were
  published in `leosavio/tap`, whose `tap_migrations.json` now points `kubestrata` to
  `kubestrata/tap`. If you installed from there, the direct path is
  `brew uninstall kubestrata && brew install kubestrata/tap/kubestrata`.

## 0.1.1 — 2026-11-26

- `kubestrata --version` prints the version without the leading `v` when installed with
  `go install` too (`0.1.1`, not `v0.1.1`), the same string as the release archives and Homebrew.
  Found by `install-smoke.yml` on 0.1.0, where the `go install` path printed `v0.1.0`.

## 0.1.0 — 2026-11-26

First release.

Distribution:

- Release binaries for darwin/linux on arm64/amd64, `checksums.txt` signed with cosign keyless
  (`release.yml`).
- Homebrew: `brew install leosavio/tap/kubestrata` (formula published by the release workflow).
- `go install github.com/kubestrata/kubestrata/cmd/kubestrata@v0.1.0`. The module path moved from
  `github.com/leosavio/kubestrata` to `github.com/kubestrata/kubestrata` before this release.
- `install-smoke.yml`: brew, go install and the signed archive on four platforms after every
  release, weekly and on demand.

Commands:

- `kubestrata contexts`: kubeconfig contexts, no cluster access.
- `kubestrata collect`: read-only, sanitized snapshot per context into the local cache, reused
  until `--max-age`, refreshed with `--force`.
- `kubestrata cache ls|prune`.
- `kubestrata render`: offline radial view, newest resources in the center, ring scales
  5d / 10d / 30d / log / custom, created or changed axis, filters, search, detail panel.
- `kubestrata compare`: two clusters side by side, states equal / drift / pending / new, coverage
  per namespace in the CLI, JSON and page.

Changes from the AWS workshop evaluation (EKS Auto Mode, Spark, Terraform):

- Ring scales `30m` and `1h` (12 rings of 30 minutes or 1 hour plus the compressed outer band) in
  the page and in `render`/`compare --scale`. Ring labels and ages below one day read in minutes
  and hours (`30m`, `1h30m`, "42 minutes", "3 hours") instead of "today".
- `--rings` takes the suffixes `m`, `h` and `d` (`--rings 30m,2h,1d,7d`); a bare number is still
  days. Rings must be strictly ascending. The page config carries them as `ring_minutes`.
- System namespaces: the default pattern names the Kubernetes ones exactly
  (`kube-system`, `kube-public`, `kube-node-lease`, `local-path-storage`) plus OpenShift and the
  cloud prefixes, so `kube-prometheus-stack` is no longer hidden. Re-collect to update cached
  snapshots.
- Managers `terraform` (field managers `HashiCorp`, `Terraform`, `terraform-provider-*`) and
  `platform` (`eks`, `eks-auto-mode`, `eks-internal`, `kubeadm`, `cluster-version-operator`, and
  the control-plane managers when they are the only writers), with colors, legend and migration
  hints in the page. At the same second, a non-platform manager wins the creator tie.
- `collect --snapshot-out <file|dir>`: the plain JSON snapshot of one context, or
  `<cluster_id>.json` per context into an existing directory. `--format json` still prints the
  summary, now with `snapshot_out`.
- `--redact` (global): masks AWS account ids (ARNs, ECR hosts, 12-digit runs in names) and EKS/AKS
  endpoint hosts in `contexts`, `collect`, `cache ls`, `diff`, `render` and `compare` output and
  error messages, in `--snapshot-out` files and in the site. The cache keeps the real values.
- `kubestrata diff -c <ctx> [--from] [--to]`: two cached snapshots of one cluster, states new /
  gone / drift / equal with the facts that changed, CLI and JSON (`"mode": "diff"`). `cache ls`
  shows a `STAMP` column (and `stamp` in JSON) for `--from`/`--to`.
- Karpenter and EKS Auto Mode facts: NodeClaim (instance type, capacity type, zone, nodepool, node
  class), NodePool (requirements, limits, weight, disruption, node class, expiry), NodeClass
  (ephemeral storage size, IOPS, throughput) and EC2NodeClass (block devices). The node summary
  counts instance types and capacity types from node labels.
- Verified with a unit fixture and on a live EKS cluster: pods owned by a custom resource
  (SparkApplication → driver pod → executor pods) fold into the custom resource with `pods`,
  `ready`, `restarts`, `archs`, `pod_newest` and `pod_oldest`.
- README quick start: `go install` puts the binary in `$(go env GOPATH)/bin`, which must be on
  `PATH`.
- `diff` and `compare` between snapshots of different collector versions count as drift only the
  facts present on both sides; the rest is listed as `collector_facts` (`collector_change` in JSON).
- `collect --snapshot-out` without `--redact` prints a note on stderr in text mode.
