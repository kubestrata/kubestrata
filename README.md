# kubestrata

[![CI](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml?query=branch%3Amain)
[![security](https://github.com/kubestrata/kubestrata/actions/workflows/security.yml/badge.svg?branch=main)](docs/security-analysis.md)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/kubestrata/kubestrata/badge)](https://scorecard.dev/viewer/?uri=github.com/kubestrata/kubestrata)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

[Português](README.pt-BR.md)

> **Status: 0.1.x, first releases.** Interfaces may still change before 1.0: see
> [docs/compatibility.md](docs/compatibility.md) for what is stable and the [CHANGELOG](CHANGELOG.md).

Time-layered inventory of Kubernetes clusters for migration planning. `kubestrata` reads the
contexts in your kubeconfig, collects a read-only, sanitized inventory into a local cache and
renders an offline HTML view: **newest resources in the center, oldest at the edge**, grouped by
namespace and team. A comparison view puts two clusters side by side, so each migration wave
shows up as a ring in the target.

- Single Go binary, no server, no account, no telemetry.
- Read-only: `list` and `get` only. Secret values never leave the cluster.
- Offline HTML: open the generated `index.html` from disk.

## Install

Homebrew (macOS and Linux), from the [`kubestrata/homebrew-tap`](https://github.com/kubestrata/homebrew-tap) tap:

```sh
brew install kubestrata/tap/kubestrata
```

Homebrew 7.0.11 and later refuse formulae from non-official taps until you trust them. If
`brew install` stops with "Refusing to load formula ... from untrusted tap", run
`brew trust --formula kubestrata/tap/kubestrata` and install again.

PyPI (macOS 12+ and Linux, arm64/amd64), with [uv](https://docs.astral.sh/uv/) or pipx. The wheel
carries the same signed Go binary as the release archive; there is no Python code:

```sh
uvx kubestrata --version        # run once, without installing
uv tool install kubestrata      # or: pipx install kubestrata
```

Go toolchain (Go 1.27 or newer). The binary lands in `$(go env GOPATH)/bin`, which must be on your
`PATH`:

```sh
go install github.com/kubestrata/kubestrata/cmd/kubestrata@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

Release archive (darwin/linux, arm64/amd64), verifying the checksum and the keyless signature:

```sh
v=0.2.0; os=darwin; arch=arm64
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

Releases from 0.2.0 on also carry GitHub artifact attestations: SLSA build provenance for every
archive and a CycloneDX SBOM per archive (`kubestrata_${v}_${os}_${arch}.cdx.json`, attached to the
release and attested). To verify without cosign, with the [GitHub CLI](https://cli.github.com):

```sh
gh attestation verify "kubestrata_${v}_${os}_${arch}.tar.gz" -R kubestrata/kubestrata
gh attestation verify "kubestrata_${v}_${os}_${arch}.tar.gz" -R kubestrata/kubestrata \
  --predicate-type https://cyclonedx.org/bom          # the SBOM attestation
```

The PyPI wheels have [PEP 740](https://peps.python.org/pep-0740/) attestations from Trusted
Publishing: PyPI shows them under **Provenance** on each file's page, and
[`pypi-attestations`](https://github.com/pypi/pypi-attestations) checks one against this
repository:

```sh
uvx pypi-attestations verify pypi --repository https://github.com/kubestrata/kubestrata \
  "pypi:kubestrata-${v}-py3-none-macosx_12_0_arm64.whl"
```

### Tested platforms

| Platform | GitHub runner | Install smoke (`install-smoke.yml`) |
|---|---|---|
| linux/amd64 | `ubuntu-24.04` | brew · go install · tarball · uv |
| linux/arm64 | `ubuntu-24.04-arm` | brew · go install · tarball · uv |
| darwin/arm64 | `macos-15` | brew · go install · tarball · uv |
| darwin/amd64 | `macos-15-intel` | brew · go install · tarball · uv |

[`install-smoke.yml`](.github/workflows/install-smoke.yml) installs a published release the way a
user does, after every release (dispatched by `release.yml`), every Tuesday and on demand
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

First time on OpenShift? [docs/openshift.md](docs/openshift.md) walks through login with the
cluster CA, the RBAC files, collect, the scales for long-lived clusters, `--redact`, what the page
shows for OpenShift, the comparison with the target and the clean-up.

### No cluster at hand? Try it on the kuberito lab

[kuberito](https://github.com/kuberito/kuberito) brings up two small local clusters on Docker, an
OpenShift-like source and an EKS-like target, with a migration in progress. It is the lab of
kubestrata's end-to-end tests ([`lab.yml`](.github/workflows/lab.yml) runs them in CI on the full
profile). Install it as its README describes (`brew install kuberito/tap/kuberito`), then:

```sh
kuberito up && eval "$(kuberito env)"     # KUBECONFIG=~/.kuberito/kubeconfig for this shell
kubestrata collect -c kuberito-source -c kuberito-target
kubestrata compare -c kuberito-source -c kuberito-target --open
```

## Permissions

kubestrata needs `list` on the kinds it inventories and `get` on the `kube-system` namespace (the
cluster identity), nothing else. Two ClusterRoles ship in [`deploy/rbac/`](deploy/rbac/), each with
a ServiceAccount and a ClusterRoleBinding in the namespace `kubestrata`:

| File | Grants | Collect with |
|---|---|---|
| [`kubestrata-reader.yaml`](deploy/rbac/kubestrata-reader.yaml) | `list` on every kind, custom resources included | `kubestrata collect` |
| [`kubestrata-reader-no-secrets.yaml`](deploy/rbac/kubestrata-reader-no-secrets.yaml) | `list` on the inventory kinds and common custom resource groups, never on Secrets | `kubestrata collect --no-secrets` |

kubestrata reads Secrets as metadata only, but RBAC has no metadata-only permission: `list` on
Secrets lets the holder read their values with another client. Without Secrets (`--no-secrets`,
the same as `--skip-kinds core/Secret`), the inventory loses the Helm release state (release,
revision and status, kept in `helm.sh/release.v1` Secrets) and the links from workloads, Ingresses
and Gateways to Secrets. Helm is still detected from the labels and annotations of the objects,
and references to Secrets are not reported as missing. A custom resource group that
`kubestrata-reader-no-secrets` does not list is reported as `Forbidden` in the snapshot errors:
add the group to the role, or leave the kind out with `--skip-kinds <group>/<Kind>`. Skipped kinds
are recorded in the snapshot (`scope.skipped_kinds`) and shown in the page.

Collect with a short-lived ServiceAccount token instead of your personal credentials, from a
kubeconfig that holds only that token (the cluster's CA comes from your current context):

```sh
kubectl apply -f deploy/rbac/kubestrata-reader-no-secrets.yaml
dir="$(mktemp -d)"; ctx=source
kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' \
  | base64 -d > "$dir/ca.crt"
KUBECONFIG="$dir/kubeconfig" kubectl config set-cluster "$ctx" --embed-certs \
  --server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')" \
  --certificate-authority="$dir/ca.crt"
KUBECONFIG="$dir/kubeconfig" kubectl config set-credentials "$ctx" \
  --token="$(kubectl create token kubestrata-reader-no-secrets -n kubestrata --duration=1h)"
KUBECONFIG="$dir/kubeconfig" kubectl config set-context "$ctx" --cluster="$ctx" --user="$ctx"
kubestrata --kubeconfig "$dir/kubeconfig" collect -c "$ctx" --no-secrets
rm -rf "$dir"
kubectl delete -f deploy/rbac/kubestrata-reader-no-secrets.yaml   # when the inventory is done
```

`render -c source` and `compare` read the cache afterwards, with no kubeconfig at all. If your
kubeconfig names a CA file (`certificate-authority`) instead of embedding it, pass that file to
`--certificate-authority`.

### Kubeconfig hygiene

`kubestrata contexts` reads the kubeconfig without touching any cluster and flags, in a `NOTES`
column (`insecure_skip_tls_verify` and `static_token` in JSON), the contexts whose credential is
exposed: a cluster with `insecure-skip-tls-verify: true` (anyone on the network path can pose as the
API server and receive your token) and a user with a static bearer token (`token` or
`tokenFile`) instead of an exec plugin or a client certificate. The token itself is never read
into the output.

To verify the API server instead of skipping the check, get the CA bundle that signs its
certificate from the cluster administrator (on OpenShift, the CA of the API server's serving
certificate), check its fingerprint out of band, then point the cluster at it and turn the check
back on:

```sh
kubectl config set-cluster <cluster> --certificate-authority=ca.crt --embed-certs \
  --insecure-skip-tls-verify=false
oc login --certificate-authority=ca.crt https://api.<cluster>.<base-domain>:6443   # or log in again with it
```

Prefer an exec plugin or a short-lived ServiceAccount token (see Permissions) to a long-lived
personal token. A token from `oc login` is an OAuth access token: run `oc logout` when the
inventory is done, which deletes it on the server and removes it from the kubeconfig.

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
| `kubestrata summary -c source` | migration summary as Markdown (or JSON): resources by category and kind, nodes by instance type and zone, managers, biggest namespaces, end-of-life kinds, objects without an expected owner, team coverage, Argo CD |

`--format json` prints a machine-readable document with `schema_version` 1, described by the JSON
Schemas in [`schemas/`](schemas/); [docs/compatibility.md](docs/compatibility.md) says what may still
change before 1.0. `--redact` masks AWS
account ids, EKS/AKS endpoint hosts, OpenShift cluster domains (`api.redacted:6443`,
`web.apps.redacted`), the API host in `oc login` context names and team names taken from
`openshift.io/requester` (user names) in the output and the site; the cache keeps the real values.

Ring scales (`render`/`compare --scale`, also switchable in the page): `30m` and `1h` (12 rings of
30 minutes or 1 hour, for young clusters), `5d`, `10d`, `30d` (the default of `compare`), `years` (30d, 90d,
180d, then 1y to 10y, for long-lived clusters), `log`, `auto` (the default of `render`: rings at
quantiles of the resource ages of the rendered snapshots, rounded: `270d,1y,1.5y,2y,2.5y,3y` on the
kuberito source), and
custom rings with unit suffixes `m`, `h`, `d` and `y`, `--rings 30m,2h,1d,7d,30d` or
`--rings 30d,90d,180d,1y,2y,3y,5y` (a bare number is days).

`compare` pairs resources by `group/Kind/namespace/name`, then pairs what the target serves with
another kind: an OpenShift Route with an HTTPRoute or an Ingress (by host, then name), an Ingress
with an HTTPRoute (Ingress to Gateway API) and a DeploymentConfig with a Deployment (by name), in
the same namespace, compared on the facts both kinds express and shown with the translation
(`Route→HTTPRoute`) in the JSON and the page. Platform kinds (OpenShift cluster operators) are
listed but left out of the counts and the coverage:

| State | Meaning |
|---|---|
| equal | in both clusters, same fingerprint |
| drift | in both, the fingerprint differs; the page and the JSON list the facts that differ |
| pending | only in the source: not migrated yet |
| new | only in the target |

Coverage per namespace = (equal + drift) / resources in the source. System namespaces are left out
of the totals. `collect --system-ns <regexp>` decides which namespaces are system (default: the
Kubernetes, OpenShift and cloud provider ones): they are still collected, only hidden by default
in the page (`system` toggle, `render`/`compare --show-system`) and left out of the totals.

### The page

The header filters by category, system namespaces, scale, axis and color; the search box (`/`)
dims what does not match. The `platform` category (custom resources of OpenShift's cluster
operators, such as MachineConfig and APIRequestCount) starts hidden: click its chip, or add
`show=platform` to the URL hash. Search syntax, the same on the render and compare pages:

| Query | Matches |
|---|---|
| `api` | one term: anywhere in the id, name, kind, app, team, manager, labels or facts, as before |
| `api, web` or `api web` | any of the terms (OR) |
| `"payments api"` | the quoted text as one term |
| `-kube` | everything except what the term matches |
| `ns:payments kind:Deployment` | a field, exactly (`*` is a wildcard): `ns:`, `kind:`, `team:`, `mgr:`, `name:`; OR within a field, AND across fields |
| `ns:payments,ns:checkout kind:Deployment -mgr:argocd` | Deployments of either namespace not managed by Argo CD |

The state lives in the URL hash (`#scale=log&color=manager&q=kind:Route`), so a view can be
bookmarked or shared with the site. `ns:_` matches cluster-scoped resources.

## Security

- Read-only by design: the kubeconfig credentials are used, never stored or printed, and Secrets
  are read as metadata only ([SECURITY.md](SECURITY.md)).
- Every change and every week: `govulncheck`, `zizmor`, CodeQL, Dependabot and secret scanning.
- Latest analysis, 2026-11-27 on 0.1.3: no known vulnerability reachable from the code or present in
  the published binary, and no open alert. 0.1.0 to 0.1.2 were built with Go 1.27.1 and carry 11
  standard-library vulnerabilities: upgrade to 0.1.3 or later.

Checks, findings history and how to repeat them on a release:
[docs/security-analysis.md](docs/security-analysis.md). To report a vulnerability, see
[SECURITY.md](SECURITY.md).

## Design

See [docs/design.md](docs/design.md), and [docs/roadmap.md](docs/roadmap.md) for the 1.0 checklist.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
