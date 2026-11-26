# kubestrata

[![CI](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml?query=branch%3Amain)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

[Português](README.pt-BR.md)

> **Status: 0.1.x, first releases.** Interfaces may still change before 1.0
> ([CHANGELOG](CHANGELOG.md)).

Time-layered inventory of Kubernetes clusters for migration planning. `kubestrata` reads the
contexts in your kubeconfig, collects a read-only, sanitized inventory into a local cache and
renders an offline HTML view: **newest resources in the center, oldest at the edge**, grouped by
namespace and team. A comparison view puts two clusters side by side, so each migration wave
shows up as a ring in the target.

- Single Go binary, no server, no account, no telemetry.
- Read-only: `list` and `get` only. Secret values never leave the cluster.
- Offline HTML: open the generated `index.html` from disk.

## Install

Homebrew (macOS and Linux), from the [`leosavio/homebrew-tap`](https://github.com/leosavio/homebrew-tap) tap:

```sh
brew install leosavio/tap/kubestrata
```

Homebrew 7.0.11 and later refuse formulae from non-official taps until you trust them. If
`brew install` stops with "Refusing to load formula ... from untrusted tap", run
`brew trust --formula leosavio/tap/kubestrata` and install again.

Go toolchain (Go 1.27 or newer). The binary lands in `$(go env GOPATH)/bin`, which must be on your
`PATH`:

```sh
go install github.com/kubestrata/kubestrata/cmd/kubestrata@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

Release archive (darwin/linux, arm64/amd64), verifying the checksum and the keyless signature:

```sh
v=0.1.1; os=darwin; arch=arm64
base="https://github.com/kubestrata/kubestrata/releases/download/v${v}"
curl -fsSLO "${base}/kubestrata_${v}_${os}_${arch}.tar.gz"
curl -fsSLO "${base}/checksums.txt"
curl -fsSLO "${base}/checksums.txt.sigstore.json"
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/kubestrata/kubestrata/.github/workflows/release.yml@refs/tags/v${v}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
grep " kubestrata_${v}_${os}_${arch}.tar.gz\$" checksums.txt | shasum -a 256 -c -
tar -xzf "kubestrata_${v}_${os}_${arch}.tar.gz" kubestrata
```

On Linux, `sha256sum -c -` does the same as `shasum -a 256 -c -`. To build from a clone instead,
`make build` writes `bin/kubestrata`.

### Tested platforms

| Platform | GitHub runner | Install smoke (`install-smoke.yml`) |
|---|---|---|
| linux/amd64 | `ubuntu-24.04` | brew · go install · tarball |
| linux/arm64 | `ubuntu-24.04-arm` | brew · go install · tarball |
| darwin/arm64 | `macos-15` | brew · go install · tarball |
| darwin/amd64 | `macos-15-intel` | brew · go install · tarball |

[`install-smoke.yml`](.github/workflows/install-smoke.yml) installs a published release the way a
user does, after every release, every Tuesday and on demand
(`gh workflow run install-smoke.yml -f version=vX.Y.Z`). For each install path it checks that
`kubestrata --version` matches the tag, that `contexts` reads a fixture kubeconfig without
printing the credential, and that `cache ls` works on an empty cache
([`scripts/smoke-check.sh`](scripts/smoke-check.sh)).

## Quick start

Run it with a source and a target context from your kubeconfig (`kubestrata contexts` lists
them):

```sh
kubestrata collect -c <source> -c <target>          # read-only inventory into the local cache
kubestrata render -c <source> --open                # radial view of one cluster
kubestrata compare -c <source> -c <target> --open   # side by side, with the coverage per namespace
```

`render` and `compare` read the cache only. Run `collect --force` to refresh it.

### No cluster at hand? Try it on the kuberito lab

[kuberito](https://github.com/kuberito/kuberito) (not public yet) brings up two small local clusters on Docker, an
OpenShift-like source and an EKS-like target, with a migration in progress. Install it as its
README describes, then:

```sh
kuberito up && eval "$(kuberito env)"     # KUBECONFIG=~/.kuberito/kubeconfig for this shell
kubestrata collect -c kuberito-source -c kuberito-target
kubestrata compare -c kuberito-source -c kuberito-target --open
```

## Screenshots

Radial view of `kuberito-source`:

![Radial view of the kuberito-source lab cluster: one sector per namespace with its team, resources colored by category, newest in the center and oldest at the edge on 30-day rings, and a deprecated legacy-portal DeploymentConfig selected with its facts and related objects in the side panel](docs/images/radial-kuberito-source.png)

Compare view of `kuberito-source` and `kuberito-target`:

![Compare view of kuberito-source and kuberito-target side by side: resources colored by state (equal green, drift amber, pending red, new blue), the drifting payments/api Deployment selected in both panes with its image tag 2.56 versus 2.57 in the side panel, and the coverage table per namespace under the panes](docs/images/compare-kuberito-source-target.png)

## Commands

| Command | Does |
|---|---|
| `kubestrata contexts` | kubeconfig contexts, no cluster access |
| `kubestrata collect -c source -c target` | read-only snapshot into the local cache; `--snapshot-out <file\|dir>` also writes it as plain JSON |
| `kubestrata cache ls` | cached snapshots, with the `STAMP` that `diff` takes |
| `kubestrata render -c source --open` | radial view, offline HTML |
| `kubestrata compare -c source -c target` | equal / drift / pending / new and coverage, side-by-side HTML |
| `kubestrata diff -c source` | two snapshots of the same cluster (newest and the one before, or `--from`/`--to`): new / gone / drift / equal |
| `kubestrata` | interactive TUI (planned) |

`--format json` prints a machine-readable document with `schema_version` 1. `--redact` masks AWS
account ids and EKS/AKS endpoint hosts in the output and the site; the cache keeps the real values.

Ring scales (`render`/`compare --scale`, also switchable in the page): `30m` and `1h` (12 rings of
30 minutes or 1 hour, for young clusters), `5d`, `10d`, `30d`, `log`, and custom rings with unit
suffixes, `--rings 30m,2h,1d,7d,30d` (a bare number is days).

`compare` pairs resources by `group/Kind/namespace/name`:

| State | Meaning |
|---|---|
| equal | in both clusters, same fingerprint |
| drift | in both, the fingerprint differs; the page and the JSON list the facts that differ |
| pending | only in the source: not migrated yet |
| new | only in the target |

Coverage per namespace = (equal + drift) / resources in the source. System namespaces are left out
of the totals.

## Design

See [docs/design.md](docs/design.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
