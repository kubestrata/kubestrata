# Changelog

All notable changes to this project are documented here. Versions follow
[Semantic Versioning](https://semver.org).

## Unreleased

No release yet. `main` has:

- `kubestrata contexts`: kubeconfig contexts, no cluster access.
- `kubestrata collect`: read-only, sanitized snapshot per context into the local cache, reused
  until `--max-age`, refreshed with `--force`.
- `kubestrata cache ls|prune`.
- `kubestrata render`: offline radial view, newest resources in the center, ring scales
  5d / 10d / 30d / log / custom, created or changed axis, filters, search, detail panel.
- `kubestrata compare`: two clusters side by side, states equal / drift / pending / new, coverage
  per namespace in the CLI, JSON and page.
