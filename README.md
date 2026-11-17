# kubestrata

[Português](README.pt-BR.md)

> **Status: pre-release.** Under active development toward v0.1.0. Interfaces may change.

Time-layered inventory of Kubernetes clusters for migration planning. `kubestrata` reads the
contexts in your kubeconfig, collects a read-only, sanitized inventory into a local cache and
renders an offline HTML view: **newest resources in the center, oldest at the edge**, grouped by
namespace and team. A comparison view puts two clusters side by side, so each migration wave
shows up as a ring in the target.

- Single Go binary, no server, no account, no telemetry.
- Read-only: `list` and `get` only. Secret values never leave the cluster.
- Offline HTML: open the generated `index.html` from disk.

## Commands (v0.1.0 target)

```sh
kubestrata contexts                       # kubeconfig contexts (available now)
kubestrata collect -c origin -c target    # read-only snapshot into the local cache
kubestrata render -c origin --open        # radial view of one cluster
kubestrata compare -c origin -c target    # side-by-side view
kubestrata                                # interactive TUI
```

## Design

See [docs/design.md](docs/design.md).

## License

[MIT](LICENSE)
