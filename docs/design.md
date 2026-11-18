# kubestrata design

[Português](design.pt-BR.md)

Status: pre-release design for v0.1.0 (F1). Decisions here are current until code says otherwise.

## Goal

Read-only inventory of the clusters in your kubeconfig, cached locally, rendered as an offline
HTML view where **recent resources sit in the center and old ones at the edge**, plus a
**side-by-side comparison of two clusters**. The audience is the engineer planning a cluster
migration: what exists, who owns it, how it got there, what has state, what is exposed, and what
is still moving.

## Flow

```mermaid
flowchart LR
  K[kubeconfig contexts] --> C[collect: client-go, list/get only]
  C --> P[projection + sanitization per kind]
  P --> S[(cache: ~/Library/Caches/kubestrata)]
  S --> R[render: offline, deterministic]
  R --> O[site: index.html + data/*.js + json/]
  O --> B[browser, file://]
```

`render` and `compare` never talk to a cluster. Refreshing the cache is always explicit
(`collect --force`, `--max-age`, or `r` in the TUI).

## Time axis

| Field | Meaning | Use |
|---|---|---|
| `metadata.creationTimestamp` | object creation; reset when CI deletes and re-applies | default axis |
| newest `managedFields[].time`, **ignoring `subresource: status`** | last spec/metadata change by any manager | `changed` axis: what is still moving |
| Pod age | last rollout or reschedule, not workload age | not an axis: pods are aggregated into their owner |

Status updates are ignored on purpose: controllers write status constantly and everything would
look recent.

## Layout: deterministic polar, not force-directed

Angle = namespace sector (grouped by team, sized by object count), sub-sector by category.
Radius = age on the selected ring scale. Edges (Ingress → Service → Workload → PVC / Secret /
ConfigMap / ServiceAccount) are drawn on hover or selection only.

| Criterion | Force-directed | Polar (chosen) |
|---|---|---|
| Scale | O(n²) repulsion, fine for hundreds | O(n); collisions resolved on a spatial grid |
| Compare | the same object lands in different places | sectors computed over A∪B: same namespace, same angle on both sides |
| Determinism | depends on the simulation | same input, same picture |

Ring scales switch in the browser without regenerating: `5d`, `10d`, `30d` (12 linear rings plus
a compressed outer band), `log` (7d / 30d / 90d / 1y+, saturating at 3 years) and custom
`--rings 5,15,30,90,180,365`.

## Collection

- Kubeconfig: client-go default loading rules (`KUBECONFIG` with several files, `--kubeconfig`);
  exec plugins work unchanged. An auth failure in one context is a structured error and does not
  stop the others.
- Cluster identity: `sha256(server URL + kube-system namespace UID)[:12]`. Context names are not
  identities.
- Discovery (`ServerPreferredResources`): served kinds, CRDs and OpenShift types without a
  hard-coded list. Distro inferred from API groups, server URL and node labels.
- API server load: `list`/`get` only, `limit=500` pagination, explicit QPS/burst, at most 4
  contexts and 8 kinds in flight per context.
- RBAC: a `403` on a kind goes to `errors[]` and collection continues. Listing Secrets grants read
  access to their values even though kubestrata only asks for metadata; the docs ship a suggested
  `inventory-reader` ClusterRole.

## Sanitization (security boundary)

The snapshot stores a curated projection, never raw objects.

| Risk | Handling |
|---|---|
| Secret values | Secrets are read through the server-side Table (`as=Table;g=meta.k8s.io;v=v1`, `includeObject=Metadata`): type and key count, no `data` |
| `kubectl.kubernetes.io/last-applied-configuration` | dropped from every object: for a Secret applied with `kubectl apply` it holds the base64 `data` |
| literal `env[].value`, `args`, `command` | names only, values dropped |
| ConfigMap | key names + content hash (for drift), no values |
| `managedFields` | only `manager`, `operation`, `time`, `subresource` |
| OpenShift Route `spec.tls.key` / `certificate` | dropped: only the termination type is kept |
| Cloud identity annotations (IRSA role ARN, workload identity ids) | only the identity type (`aws-irsa`, ...), never the ARN or id |
| Annotations in general | not stored; facts are derived from the few that matter |
| Cache and site | `0700` / `0600` under the user cache dir, never in git |
| XSS in the local page | names and labels are untrusted: `json.Marshal` (escapes `<>&`) writes `data/*.js`, the page renders with `textContent` |

A test seeds known values into Secrets and env vars and fails if any of them appears in any byte
of the cache or the site.

## Default kinds and categories

| Category | Kinds | Notes |
|---|---|---|
| workload | Deployment, StatefulSet, DaemonSet, CronJob, Job without owner, DeploymentConfig, orphan Pod and ReplicaSet | pods aggregated into their owner; an orphan pod is a migration warning |
| network | Service (minus `default/kubernetes`), Ingress, Route, Gateway, HTTPRoute, NetworkPolicy | hosts and TLS in the panel |
| config | ConfigMap (minus `kube-root-ca.crt`, `openshift-service-ca.crt`), Secret, ServiceAccount | `helm.sh/release.v1` Secrets grouped as Helm state |
| storage | PVC, PV, StorageClass, VolumeSnapshot | cluster-scoped kinds get their own sector |
| policy | HPA, PDB, ResourceQuota, LimitRange, Role, RoleBinding | PDBs affect drain and cutover |
| CRDs | custom resources in application namespaces | grouped by API group |
| system | namespaces matching `^(kube-|openshift|amazon-|aws-|gke-|azure-)` | hidden by default (`--exclude-ns`) |

## Snapshot contract (`schema_version: 1`)

```json
{
  "schema_version": 1,
  "tool": {"name": "kubestrata", "version": "0.1.0"},
  "collected_at": "2026-11-17T12:12:00Z",
  "cluster": {"id": "3f9a1c0b7e21", "context": "source", "server_host": "api.example:6443",
              "distro": "openshift", "distro_signals": ["group route.openshift.io"], "version": "v1.27.x",
              "nodes": {"count": 12, "arch": {"amd64": 12}, "zones": ["a", "b"]}},
  "namespaces": [{"name": "payments", "team": "team-payments", "created": "2024-06-01T00:00:00Z"}],
  "resources": [{
    "id": "apps/Deployment/payments/api", "cat": "workload", "ns": "payments", "name": "api",
    "created": "2025-04-24T10:00:00Z", "changed": "2026-10-02T18:30:00Z",
    "manager": "helm", "app": "payments-api", "team": "team-payments",
    "facts": {"replicas": 3, "ready": 3, "pods": 3, "restarts": 1, "images": ["registry/pay/api:2.14"]},
    "fp": "sha256:..."
  }],
  "edges": [{"from": "networking.k8s.io/Ingress/payments/api", "to": "core/Service/payments/api", "rel": "routes"}],
  "errors": [{"kind": "core/Secret", "ns": "data", "reason": "Forbidden"}]
}
```

| `rel` | From → to |
|---|---|
| `routes` | Ingress, Route, HTTPRoute → Service |
| `selects` | Service, PodDisruptionBudget → workload (selector ⊆ pod template labels) |
| `mounts` | workload → PVC, Secret, ConfigMap (volumes); VolumeSnapshot → PVC |
| `env-from` | workload → Secret, ConfigMap (`envFrom`, `valueFrom`) |
| `uses-sa` | workload → ServiceAccount |
| `pull-secret` | workload → Secret (`imagePullSecrets`) |
| `owns` | controller owner → shown object (e.g. a custom resource → its Deployment) |
| `binds` | PVC → PersistentVolume, StorageClass |
| `tls` | Ingress, Gateway → Secret |
| `scales` | HPA → target |
| `grants` | RoleBinding → Role, ServiceAccount |
| `attaches` | HTTPRoute → Gateway |

Edges are computed in Go, not in the browser. A reference to an object that does not exist goes to
`facts.missing_refs`; a Service whose selector matches no workload gets `facts.selects_none`. A
consumer that receives a higher `schema_version` than it supports refuses the document.

Pods, ReplicaSets, ReplicationControllers and Jobs are folded into the nearest shown owner
(`pods`, `ready`, `restarts`, `pod_newest`, `pod_oldest`, `archs`, `last_rollout`, `jobs`,
`last_run`). Without an owner they are shown with `facts.orphan`.

## Comparison

`kubestrata compare -c <source> -c <target>` reads the newest cached snapshot of each context and
never talks to a cluster. The comparison is computed in Go (`internal/compare`), so the CLI table,
the JSON document and the page show the same numbers. The JSON keeps coverage with 4 decimals; the
CLI (`math.Round`) and the page (`Math.round`) both round the percentage half up, so 0.125 shows
as 13% in both.

Identity key: the resource id `group/Kind/namespace/name`. States:

| State | Meaning | Color |
|---|---|---|
| equal | in both, same fingerprint (`fp`) | `#66bb6a` |
| drift | in both, different fingerprint; the detail lists the facts that differ | `#ffb300` |
| pending | only in the source: not migrated yet | `#ef5350` |
| new | only in the target | `#42a5f5` |

The fingerprint is a hash of the kind and its projected facts **minus a volatile set** (pod
counts, readiness, restarts, pod and rollout times, Job runs, architectures, field managers,
missing refs, Helm revision and status, orphan flags). A full-spec hash would flag defaulting
differences between Kubernetes versions; the projection per kind decides what counts:

| Kind | Facts in the fingerprint (main ones) |
|---|---|
| Deployment, StatefulSet, DaemonSet, DeploymentConfig | images, replicas, CPU and memory requests/limits, service account, PVC names, node selector (DeploymentConfig also strategy and triggers) |
| Service | type, ports |
| Ingress, Route, HTTPRoute | hosts, paths, TLS secret names, class |
| PVC | storageClass, size, accessModes |
| ConfigMap | key names + content hash |
| Secret | type + key count |
| ServiceAccount | cloud identity type (`aws-irsa`, ...), image pull secret count |

Volume, envFrom and Secret/ConfigMap references are edges, not facts: they do not enter the
fingerprint.

Drift detail = every fact outside the volatile set whose value differs, with both values (a side
without the fact has none). Because the key holds group/Kind, a **kind translation** shows as a
pair: an OpenShift Route or an Ingress replaced by a Gateway API HTTPRoute is pending in the source
and new in the target. Pairing across kinds belongs to the migration units of F2. Objects with
generated names (PersistentVolumes `pvc-<uid>`) never pair either.

Per-namespace summary (`_` = cluster-scoped): source total, target total, equal, drift, pending,
new and coverage = (equal + drift) / source total, `null` when the source has nothing there.
Totals and the default table leave system namespaces out, like the default view.

`--format json` prints one document (`schema_version` 1) with `source`, `target`, `totals`,
`namespaces`, `items` (`id`, `ns`, `kind`, `state`, `diff[]`) plus `index` and `url` of the site:

```json
{"schema_version": 1,
 "source": {"cluster_id": "d1f6c4078d11", "context": "source", "distro": "openshift", "resources": 406},
 "totals": {"ns": "total", "source": 320, "target": 147, "equal": 90, "drift": 44, "pending": 186, "new": 13, "coverage": 0.4188},
 "items": [{"id": "apps/Deployment/payments/api", "ns": "payments", "kind": "Deployment", "state": "drift",
            "diff": [{"fact": "images", "source": ["registry/api:2.56"], "target": ["registry/api:2.57"]}]}]}
```

The page puts the two clusters side by side with sectors computed over both (same namespace, same
angle), synchronized zoom and pan, and the color mode `state` by default (category, manager and
team stay available). Hovering or selecting a resource highlights the same id in both panes; the
side panel shows both sides and the differing facts. A coverage table under the panes has one row
per namespace. Clicking a row sets the namespace filter (`ns=<namespace>` in the URL hash, `_` for
cluster-scoped): both panes and the sectors keep only the resources whose namespace is exactly
that one, the row stays highlighted and a chip in the header names the filter. Clicking the same
row again, or the chip, clears it. The filter is independent of the search (`q`), which still dims
what does not match. The footer counts each identity once over both panes; its state counts
follow the active filters (categories, system, namespace), and its coverage is the total over all
non-system namespaces, the same as the CLI total.

## CLI and TUI

Fang v2 over Cobra for commands; Bubble Tea v2, Bubbles v2, Lip Gloss v2 and Huh for the TUI.
`--format text|json` on every command.

| Command | Does |
|---|---|
| `kubestrata` | TUI: contexts, cache age, status; `r` refresh, `g` generate, `c` compare, `o` open |
| `kubestrata contexts` | kubeconfig contexts (no cluster access) |
| `kubestrata collect [-c ctx]... [--all] [--kinds ...] [--max-age 6h] [--force]` | writes the cache; reuses a snapshot newer than `--max-age` |
| `kubestrata render [-c ctx] [--scale 5d\|10d\|30d\|log] [--axis created\|changed] [-o dir] [--open]` | site from the cache |
| `kubestrata compare -c source -c target [--scale ...] [--axis ...] [-o dir] [--open]` | coverage table on stdout and side-by-side site from the cache |
| `kubestrata cache ls\|prune [--keep 5]` | snapshot housekeeping |

The page state lives in the URL hash (`#scale=log&axis=changed&color=manager&ns=<namespace>&sel=<id>`),
so a view can be bookmarked and checked headless; `ns=` works in the single-cluster view too. A
Content-Security-Policy (`default-src 'none'`) keeps the page off the network. After the first draw
the page writes `data-status`, `data-shown` (resources drawn, summed over the panes),
`data-draw-ms` and `data-total-ms` on `<body>` for automated checks. `make page-check` writes both
sites from the cache, loads them in headless Chrome for several hash states and fails unless every
page reports `data-status="ok"`. Measured in headless Chrome on an M4: 20 000 resources draw in
about 10–16 ms (first paint about 50 ms, 4.8 MB data file).

Site output (default `<cache dir>/site` for `render`, `<cache dir>/compare` for `compare`):
`index.html` (inline CSS and JS, no CDN), `data/<cluster_id>.js` (`window.KUBESTRATA["<id>"] = {...}`;
a script tag, because Chrome blocks `fetch()` of local JSON on `file://`), `json/<cluster_id>.json`
for machines; `compare` adds `data/compare.js` (`window.KUBESTRATA_COMPARE = {...}`) and
`json/compare.json`.

## Phases

| Phase | Scope | Done when |
|---|---|---|
| F1 (v0.1.0) | contexts, collect + cache, render, compare, basic TUI, JSON output | e2e green on two local clusters; golden HTML identical across runs; no-leak test green; page loads with no JS error in headless Chrome; 20k nodes pan and zoom measured |
| F2 | migration units, classification (manager, state, exposure, cloud coupling, end of life), `plan --format json\|csv\|md`, per-team brief, `--map-ns` | plan for the fixture matches a fixed ground truth |
| F3 | temporal diff between snapshots, multi-arch image check, `kubectl strata` via Krew | decided once F2 is in real use |
