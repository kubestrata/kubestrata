# Contributing

[Português](CONTRIBUTING.pt-BR.md)

Issues and pull requests are welcome. Short rules:

| Topic | Rule |
|---|---|
| Scope | read-only inventory, offline view, comparison. A new verb on the cluster (anything beyond `list`/`get`) needs an issue first |
| Sanitization | a new kind or fact must not carry values from Secrets, env, args, commands or annotations; extend the leak test in `internal/collect` |
| Determinism | same snapshot, same output bytes: no clock, no unsorted map iteration |
| Snapshot contract | fields only grow; a change of meaning bumps `schema_version` and readers refuse newer versions |
| Web view | no network (CSP), untrusted strings through `textContent` only; run `make page-check` |
| Checks | `make check` (gofmt, vet, staticcheck, `go test -race`, build); for collection, compare or page changes, on a [kuberito](https://github.com/kuberito/kuberito) lab (`brew install kuberito/tap/kuberito && kuberito up`), `make lab-test` and `make page-check`, which CI also runs in `lab.yml` |
| Workflows | actions pinned to a full commit SHA with the tag in a comment; minimal `permissions:`; `persist-credentials: false` |
| Commits | English, imperative mood (`feat:`, `fix:`, `docs:`, `chore:`) |
| Docs | English files are the official version; keep the `*.pt-BR.md` translation in sync in the same PR when possible |

Design: [docs/design.md](docs/design.md).
