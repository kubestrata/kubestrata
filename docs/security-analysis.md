# Security analysis

[Português](security-analysis.pt-BR.md)

This page lists the security checks that run on kubestrata, how to repeat them on a release you
downloaded, and every finding so far with what was done about it. What kubestrata reads, stores
and never prints, and how to report a vulnerability, are in [SECURITY.md](../SECURITY.md).

Results below are dated. The checks keep running in CI, and the repository's **Security** tab
shows the alerts open right now.

## Checks

| Check | Finds | Runs | Blocks |
|---|---|---|---|
| [`govulncheck`](https://go.dev/doc/security/vuln/) v1.10.0 ([`security.yml`](../.github/workflows/security.yml)) | known vulnerabilities from the Go vulnerability database, in the standard library and in modules, that kubestrata's code reaches (call graph) | every pull request, every push to `main`, weekly | yes, on any reachable vulnerability |
| [`zizmor`](https://docs.zizmor.sh/) 1.33.1 ([`security.yml`](../.github/workflows/security.yml)) | GitHub Actions weaknesses: permissions, dangerous triggers, template injection, environment files, unpinned actions | every pull request, every push to `main`, weekly | yes, on any finding |
| CodeQL (repository settings, default setup) | static analysis of the Go code, the page JavaScript and the workflows | every pull request, every push to `main`, weekly | alerts on the pull request and in **Security** |
| Dependabot alerts and security updates | advisories for the modules in `go.mod` and for the actions | continuously | opens a pull request with the fix |
| Dependabot version updates ([`dependabot.yml`](../.github/dependabot.yml)) | newer modules (Kubernetes and `golang.org/x` grouped) and actions | weekly | opens a pull request |
| Secret scanning and push protection | credentials committed to the repository | on every push | a push with a detected secret is rejected |
| `go vet`, `staticcheck` ([`ci.yml`](../.github/workflows/ci.yml)) | bug patterns | every pull request, every push to `main` | yes |
| [`install-smoke.yml`](../.github/workflows/install-smoke.yml) | the installed binary never prints the kubeconfig credential (`contexts` on a fixture with a token) | after every release, weekly | yes |

The standard-library findings depend on the Go version that builds the binary, which is the `go`
line of [`go.mod`](../go.mod) (the release workflow reads it). A new Go advisory fails the weekly
`govulncheck` run even without a change here; the fix is a toolchain bump and a release.

## Supply chain

- Every action is pinned to a commit SHA. Workflows default to `contents: read`; only the release
  job (`contents: write`, `id-token: write`) and the job that starts the install smoke
  (`actions: write`) get more.
- Release archives are built with `CGO_ENABLED=0`, `-trimpath` and fixed archive metadata.
  `checksums.txt` is signed with cosign keyless (Sigstore, GitHub OIDC); the README shows how to
  verify an archive.
- The Homebrew formula pins each archive by `sha256`. The release workflow updates
  [kubestrata/homebrew-tap](https://github.com/kubestrata/homebrew-tap) with a deploy key that can
  write to that repository only.

## Repeat the checks on a release

```sh
v=0.1.3
gh release download "v${v}" -R kubestrata/kubestrata -p "kubestrata_${v}_linux_amd64.tar.gz"
tar -xzf "kubestrata_${v}_linux_amd64.tar.gz" kubestrata
go version kubestrata                                   # Go toolchain the binary was built with
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 -mode binary ./kubestrata
```

From a clone, `go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 -show verbose ./...` also lists
vulnerabilities in modules the code does not call.

## Findings

### 2026-11-26, version 0.1.2 (commit `9f1f2b6`)

| Tool | Finding | Outcome |
|---|---|---|
| `govulncheck` | 11 standard-library vulnerabilities reachable with Go 1.27.1, in `net/http`, the HTTP/2 client, `crypto/tls`, `crypto/x509` and `net/textproto`: GO-2026-6859, GO-2026-6809, GO-2026-6617, GO-2026-6613, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6608, GO-2026-6607, GO-2026-6605, GO-2026-6603. Fixed in Go 1.27.2 to 1.27.4 | fixed in 0.1.3: built with Go 1.27.4 |
| `govulncheck` | 7 more in modules, not reached by the code (`golang.org/x/net` v0.57.0 among them) | fixed in 0.1.3: `golang.org/x/net` v0.62.0 |
| `zizmor` | `dangerous-triggers` (`workflow_run` in `install-smoke.yml`) and 3 × `github-env` (writes to `GITHUB_ENV`) | fixed in 0.1.3: the release workflow dispatches the smoke, values pass through step outputs |
| [`gosec`](https://github.com/securego/gosec) | 6 findings, none critical | accepted, see below |

Versions 0.1.0 to 0.1.2 carry the standard-library vulnerabilities above. Upgrade to 0.1.3 or later.

### 2026-11-27, version 0.1.3 (commit `6b4a976`)

| Tool | Result |
|---|---|
| `govulncheck` on the source, `-show verbose` | no vulnerabilities |
| `govulncheck -mode binary` on the published linux/amd64 archive | no vulnerabilities; `go version` reports go1.27.4 |
| CodeQL (Go, Actions, JavaScript/TypeScript) | 0 alerts |
| Dependabot alerts, secret scanning | 0 open |
| `zizmor` | no findings |

### Accepted `gosec` findings

`gosec` ran by hand on 0.1.2; it is not part of CI. Each finding, with why it stays:

| Rule | Where | Why it is accepted |
|---|---|---|
| G204, subprocess with a variable | `internal/cli/render.go`, `openBrowser` | runs only with `--open`. The program is fixed (`open` on macOS, `xdg-open` elsewhere) and the argument is the path of the page kubestrata just wrote. No shell is involved. |
| G302, permissions above `0600` | `internal/cache/cache.go`, `os.Chmod(dir, 0o700)` | a directory needs the execute bit to be entered; `0700` is owner-only. Cache files are `0600`. |
| G104, unchecked error (4 ×) | `internal/cache/cache.go` `writeAtomic` and `internal/cli/collect.go` snapshot writer: `tmp.Close()` on an error path | the function already returns the first error, and a deferred `os.Remove` deletes the temporary file, so a second error from `Close` adds nothing. |
