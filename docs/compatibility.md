# Compatibility

[Português](compatibility.pt-BR.md)

kubestrata is before 1.0. This page says which outputs you can build automation on today, what may
still change, and the policy proposed from 1.0 on.

## What `schema_version` covers

Every JSON document kubestrata prints or writes carries `schema_version`, and each one has a JSON
Schema (draft 2020-12) in [`schemas/`](../schemas/):

| Document | Produced by | Schema |
|---|---|---|
| snapshot | the cache files, `collect --snapshot-out`, the site's `json/<cluster_id>.json` | [`snapshot.schema.json`](../schemas/snapshot.schema.json) |
| contexts | `contexts --format json` | [`contexts.schema.json`](../schemas/contexts.schema.json) |
| collect | `collect --format json` | [`collect.schema.json`](../schemas/collect.schema.json) |
| cache ls | `cache ls --format json` | [`cache-ls.schema.json`](../schemas/cache-ls.schema.json) |
| render | `render --format json` | [`render.schema.json`](../schemas/render.schema.json) |
| compare | `compare --format json`, the site's `json/compare.json` | [`compare.schema.json`](../schemas/compare.schema.json) |
| diff | `diff --format json` | [`diff.schema.json`](../schemas/diff.schema.json) |
| summary | `summary --format json` | [`summary.schema.json`](../schemas/summary.schema.json) |

The tests check the real outputs against these schemas, strictly: a field the code writes and the
schema does not list fails the build (`go test ./schemas`, and the lab e2e for the snapshots and
the comparison of the kuberito lab).

Within `schema_version` 1:

- Fields only grow. A new field is optional; an existing field keeps its name, type and meaning.
- The closed value sets, marked `enum` in the schemas, do not change: the states of `compare`
  (`equal`, `drift`, `pending`, `new`) and `diff` (`equal`, `drift`, `gone`, `new`), the `collect`
  status, the `summary` unowned reasons, `mode: "diff"`.
- The open value sets may gain values: `cat`, `manager`, the edge `rel`, the fact names in
  `facts` and their values, `distro`, the `auth` of a context. Treat an unknown value as "other".
- A consumer ignores fields it does not know and refuses a `schema_version` higher than it supports.
- A change of meaning moves `schema_version` to 2, and kubestrata itself refuses a newer snapshot
  than it reads.

## What may change before 1.0

- Flags, their defaults (the default scale, `--max-age`) and command names; a flag may be renamed
  or dropped, with a note in the [CHANGELOG](../CHANGELOG.md).
- The text output (tables, Markdown of `summary`, notes on stderr): it is for people, not for
  parsing. Use `--format json`.
- The projection of each kind: which facts exist, their names inside `facts`, and therefore the
  fingerprints. Between snapshots of two versions, `compare` and `diff` count only the facts both
  sides have and report the rest in `collector_change` / `collector_facts`.
- The page: the view config embedded in `index.html`, the URL hash parameters, the layout and the
  `data-*` attributes of `<body>`.
- The cache layout under the cache directory (`clusters/<id>/<stamp>.json.gz`, `contexts.json`).
- `schema_version` itself, if a document needs a change of meaning before 1.0.

## Proposed policy from 1.0

- Semantic versioning of the binary. Within a major version, JSON documents change only by
  addition (new optional fields, new values in the open sets); `schema_version` moves only with a
  major version.
- A flag to be removed or renamed is deprecated first: listed in the CHANGELOG, still accepted with
  a warning on stderr for at least one minor release, removed only in a major release.
- Exit codes stay: 0 on success, non-zero when a command fails or a collection fails for a context.
- The text output and the page may change in any release.
- The schemas are published with each release, at the same tag.
