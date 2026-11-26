# Changelog

All notable changes to this project are documented here. Versions follow
[Semantic Versioning](https://semver.org).

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
