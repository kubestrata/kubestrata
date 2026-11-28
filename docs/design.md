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

Ring scales switch in the browser without regenerating: `30m`, `1h`, `5d`, `10d`, `30d` (12
linear rings, 30 min → 6 h, 1 h → 12 h, 5 d → 60 d and so on, plus a compressed outer band up to 3
years), `years` (30d, 90d, 180d, 1y, 2y, 3y, 4y, 5y, 7y, 10y), `log` (7d / 30d / 90d / 1y+,
saturating at 3 years), `auto` and custom rings (`--rings 30m,2h,1d,7d,30d`; the suffixes are `m`,
`h`, `d` and `y` (365 days), and a bare number is days, so `--rings 5,15,30,90,180,365` keeps its
meaning). The hour scales are for young clusters: a cluster created yesterday puts everything
inside the first 5-day ring. `years` and day and year rings (`--rings 30d,90d,180d,1y,2y,3y,5y`)
are for long-lived clusters, where most resources are older than a year and would pile up in the
outer band of the linear scales. Ring labels and ages below one day read in minutes and hours
(`30m`, `1h30m`, "42 minutes", "3 hours"); whole and half years read `1y`, `1.5y`.

`auto` puts one ring at each eighth of the age distribution of the rendered snapshots (both, in a
comparison; system namespaces left out unless nothing else is left), the last one at the oldest
resource, each rounded on a log scale to a readable value (5m … 12h, 1d … 270d, 1y, 1.5y, 2y,
2.5y, 3y … 100y); rings that round to the same value merge, and a resource past the rounded last
ring sits in the compressed band. `render` computes the rings for both axes into the page config
(`auto_ring_minutes`), so switching the axis keeps them right. On the kuberito lab, `auto` reads
`270d,1y,1.5y,2y,2.5y,3y` for the source alone and spreads the old source evenly, but in the
comparison it squeezes the target's migration waves (20, 65 and 140 days) into the first rings,
which `30d` keeps apart, so `30d` stays the default.

## Collection

- Kubeconfig: client-go default loading rules (`KUBECONFIG` with several files, `--kubeconfig`);
  exec plugins work unchanged. An auth failure in one context is a structured error and does not
  stop the others.
- Cluster identity: `sha256(server URL + kube-system namespace UID)[:12]`. Context names are not
  identities.
- Discovery (`ServerPreferredResources`): served kinds, CRDs and OpenShift types without a
  hard-coded list. Distro inferred from API groups, server URL and node labels.
- Nodes are not inventory items: the cluster gets a node summary (count, architectures, zones,
  kubelet versions, and instance-type and capacity-type counts from the node labels
  `node.kubernetes.io/instance-type`, `karpenter.sh/capacity-type`,
  `eks.amazonaws.com/capacityType`, the GKE Spot/preemptible and AKS Spot labels).
- API server load: `list`/`get` only, `limit=500` pagination, explicit QPS/burst, at most 4
  contexts and 8 kinds in flight per context.
- RBAC: a `403` on a kind goes to `errors[]` and collection continues. Listing Secrets grants read
  access to their values even though kubestrata only asks for metadata. Two ClusterRoles ship in
  `deploy/rbac/`: `kubestrata-reader` (`list` on every kind, `get` on the `kube-system` namespace)
  and `kubestrata-reader-no-secrets` (the inventory kinds and common custom resource groups, no
  Secrets), for `collect --no-secrets`. Neither grants `get` on other objects, so no subresource
  (`pods/exec`, `nodes/proxy`) is reachable.
- `--skip-kinds Kind,group/Kind` (`core` for the core group, case-insensitive) leaves kinds out;
  `--no-secrets` is `--skip-kinds core/Secret`. Skipped kinds go to `scope.skipped_kinds`, and a
  reference to a skipped kind is not a `missing_refs` entry. Without Secrets the Helm release state
  (`helm.sh/release.v1` Secrets) and the edges to Secrets are lost; Helm is still detected from
  labels and annotations.

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

### Redaction (`--redact`)

Sanitization keeps secrets out of the cache. What remains still identifies the account: an EKS
context and user are usually the cluster ARN, with the 12-digit AWS account id, and the endpoint
host is unique to the cluster. The global flag `--redact` masks them in what kubestrata prints and
writes for sharing, and leaves the cache as it is:

| Masked | Example |
|---|---|
| account id in an ARN | `arn:aws:eks:us-west-2:redacted:cluster/prod` |
| account id in an ECR registry host | `redacted.dkr.ecr.us-west-2.amazonaws.com/app:1.2` |
| first label of an EKS or AKS endpoint | `redacted.gr7.us-west-2.eks.amazonaws.com`, `redacted.hcp.eastus.azmk8s.io` |
| any run of exactly 12 digits in a context, cluster or user name | `redacted_AdministratorAccess` |
| `<cluster>.<base>` of an OpenShift API host, `api.<cluster>.<base>` or `api-int.<cluster>.<base>` | `api.redacted:6443`, `api-int.redacted` |
| `<cluster>.<base>` of any host under `apps.<cluster>.<base>` (Route, Ingress and HTTPRoute hosts, messages) | `web-ns-a.apps.redacted` |
| the dashed API host that `oc login` writes into cluster, user and context names | `ns-a/api-redacted:6443/user-a` |
| a team taken from the namespace annotation `openshift.io/requester` (a user name) | `redacted-1`, `redacted-2` |

It applies to `contexts` (text and JSON), `collect` output and `--snapshot-out` files, `cache ls`,
`diff`, and the `render` and `compare` output and site (every string of the snapshots and of the
comparison, error messages included). Every command's error, printed on stderr, goes through the
same mask, since it often names the context. `compare` and `diff` pair the real snapshots first and
mask the result, so the states do not change. Cluster ids are hashes and stay as they are. An
endpoint is a host with at least two labels between the cluster label and `eks.amazonaws.com` or
`azmk8s.io`, so API groups such as `metrics.eks.amazonaws.com` are not masked.

OpenShift host shapes: `api` and `api-int` must be the first label of the host, `apps` must be a
whole label (it may follow others, as in `web-ns-a.apps.<cluster>.<base>`), and at least three
labels must follow them, so API groups and CRD names (`apps.openshift.io`,
`deploymentconfigs.apps.openshift.io` in a Forbidden message, `clonesets.apps.kruise.io`,
`api.cerbos.dev`) keep their names. A cluster under a one-label base domain (`api.ocp.lab`) and
custom Route domains (`shop.example.com`) are not recognized. `oc login` names the cluster
`api-<cluster>-<base with dashes>:<port>`, the user `<user>/<cluster>` and the context
`<namespace>/<cluster>/<user>`; the dashed host must have at least two parts after `api`, and the
user part is left as it is. Team names from `openshift.io/requester` become `redacted-1`,
`redacted-2`, ... numbered over every snapshot of the run, so the same user gets the same mask in
both panes of a comparison and in its JSON; the snapshot records where each namespace team came
from (`team_from`), and snapshots collected before 0.2 lack it, so collect again before sharing.
Without `--redact`,
`collect --snapshot-out` prints a note on stderr (text output): the file leaves the cache, so it is
the output most likely to be shared.

## Default kinds and categories

| Category | Kinds | Notes |
|---|---|---|
| workload | Deployment, StatefulSet, DaemonSet, CronJob, Job without owner, DeploymentConfig, orphan Pod and ReplicaSet | pods aggregated into their owner; an orphan pod is a migration warning |
| network | Service (minus `default/kubernetes`), Ingress, Route, Gateway, HTTPRoute, NetworkPolicy | hosts and TLS in the panel |
| config | ConfigMap (minus `kube-root-ca.crt`, `openshift-service-ca.crt`), Secret, ServiceAccount | `helm.sh/release.v1` Secrets grouped as Helm state |
| storage | PVC, PV, StorageClass, VolumeSnapshot | cluster-scoped kinds get their own sector |
| policy | HPA, PDB, ResourceQuota, LimitRange, Role, RoleBinding | PDBs affect drain and cutover |
| CRDs | custom resources in application namespaces | grouped by API group; Karpenter and EKS Auto Mode kinds get their own facts (below) |
| system | namespaces matching `^(kube-system\|kube-public\|kube-node-lease\|local-path-storage\|openshift(-.*)?\|amazon-.*\|aws-.*\|gke-.*\|azure-.*)$` | hidden by default (`collect --system-ns`); the Kubernetes ones by exact name, so `kube-prometheus-stack` is an application |

The system flag is set at collection time: snapshots cached before a change of the pattern keep
their flags until the next `collect`.

Karpenter and EKS Auto Mode facts come from labels and spec, never from status (the NodeClaim node
name is the only status value read, and it is volatile):

| Kind | Facts |
|---|---|
| `karpenter.sh` NodeClaim | `instance_type`, `capacity_type`, `zone`, `nodepool` (labels, or a single-valued `In` requirement), `node_class`, `node` |
| `karpenter.sh` NodePool | `requirements` (`key Op v1,v2`), `limits` (`cpu=1000`), `weight`, `node_class`, `expire_after`, `consolidation_policy`, `consolidate_after`, `disruption_budgets` |
| `eks.amazonaws.com` NodeClass | `ephemeral_storage` (`size`, `iops`, `throughput`) |
| `karpenter.k8s.aws` EC2NodeClass | `block_devices` (`device`, `type`, `size`, `iops`, `throughput`) |

## Managers

`manager` says how an object got into the cluster, which drives its migration path. Explicit
labels and annotations win (Argo CD, Flux, Helm), then ownership by a custom resource (operator),
then the field manager of the oldest non-status `managedFields` entry. Control-plane managers
(`kube-controller-manager`, `kube-apiserver`, `kubelet`, `kube-scheduler`,
`cloud-controller-manager`) count as creator only when no other manager wrote the object; at the
same second a non-platform manager wins over a platform one (EKS Auto Mode writes defaults into the
NodePools Terraform creates).

| Manager | From | Migration hint in the page |
|---|---|---|
| argocd, flux | labels and annotations; field managers containing `argocd`, `kustomize-controller`, `helm-controller` | GitOps: re-target the destination |
| helm | labels, annotations, Helm release Secrets | reinstall the release in the target |
| terraform | field managers `HashiCorp` (kubernetes and kubectl providers), `Terraform` (server-side apply), `terraform-provider-*` | IaC: re-target the Terraform code to the new cluster |
| operator | controller owner is a custom resource | migrate the custom resource, not its children |
| kapp | label `kapp.k14s.io/app`; field managers `kapp`, `kapp-controller` (Carvel) | Carvel kapp: deploy the same kapp app (or kapp-controller App) to the target |
| kubectl | `kubectl*` | applied by hand: export, clean, move to Git |
| console | field managers starting with `Mozilla`: the API server names a field manager after the user agent, so an edit in the OpenShift (or another) web console arrives as `Mozilla` | edited in a web console: a manual change, review and codify it before migrating |
| platform | `eks`, `eks-auto-mode`, `eks-internal`, `kubeadm`, `cluster-version-operator`, and the control-plane managers above when they are the only writers | recreated by the target platform, do not migrate (for a PV or PVC: the object is recreated, move the data) |
| other | any other field manager (listed in `facts.field_managers`; the creating one in `facts.manager_raw`, and the page lists the five most common) | unknown tool: check managedFields |
| unknown | no `managedFields` | no manager data |

Argo CD can track what it deploys by a label (`argocd.argoproj.io/instance`, or the label an
instance configures), by the annotation `argocd.argoproj.io/tracking-id`, or both, and an instance
may use none of the defaults above. So when the cluster serves `argoproj.io` Applications,
kubestrata also reads each Application's `status.resources` (group, kind, namespace, name) and
gives the manager `argocd` to every collected object listed there, over any other manager. An
Application whose destination is another cluster (`spec.destination.server` other than
`https://kubernetes.default.svc`, or a `name` other than `in-cluster`) lists that cluster's
objects: it is counted as remote and not used. The cluster then gets `argocd`
(`applications`, `remote`, `managed`, `from_status`); `managed: 0` shows in the page as "Argo CD
installed, no managed objects found": the Applications deploy elsewhere, cannot be read (see
`errors`), or track nothing that was collected. With `--crds=false` the Applications are not
listed.

Neither `manager` (a resource field) nor `field_managers` (a volatile fact) enters the
fingerprint, so a change of tool alone is not a drift.

## Snapshot contract (`schema_version: 1`)

```json
{
  "schema_version": 1,
  "tool": {"name": "kubestrata", "version": "0.1.0"},
  "collected_at": "2026-11-17T12:12:00Z",
  "cluster": {"id": "3f9a1c0b7e21", "context": "source", "server_host": "api.example:6443",
              "distro": "openshift", "distro_signals": ["group route.openshift.io"], "version": "v1.27.x",
              "nodes": {"count": 12, "arch": {"amd64": 12}, "instance_types": {"m6a.large": 12},
                        "capacity_types": {"on-demand": 10, "spot": 2}, "zones": ["a", "b"]},
              "argocd": {"applications": 3, "managed": 41, "from_status": 12}},
  "namespaces": [{"name": "payments", "team": "team-payments", "team_from": "team", "created": "2024-06-01T00:00:00Z"}],
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
`last_run`). The owner chain is walked up to the first shown object, so a custom resource gets
the facts too: a SparkApplication owns its driver pod, the driver owns the executor pods, and all
of them count in the SparkApplication. Without an owner they are shown with `facts.orphan`.

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
counts, readiness, restarts, pod and rollout times, Job runs, architectures, phases, node and
volume names, field managers, missing refs, Helm revision and status, orphan flags). A full-spec
hash would flag defaulting differences between Kubernetes versions; the projection per kind
decides what counts:

| Kind | Facts in the fingerprint (main ones) |
|---|---|
| Deployment, StatefulSet, DaemonSet, DeploymentConfig | images, replicas, CPU and memory requests/limits, service account, PVC names, node selector (DeploymentConfig also strategy and triggers) |
| Service | type, ports |
| Ingress, Route, HTTPRoute | hosts, paths, TLS (Ingress `tls`, Route termination), backends (`service:port`, the port resolved to the Service port when the Service is in the snapshot), class |
| PVC | storageClass, size, accessModes |
| ConfigMap | key names + content hash |
| Secret | type + key count |
| ServiceAccount | cloud identity type (`aws-irsa`, ...), image pull secret count |
| NodeClaim, NodePool, NodeClass, EC2NodeClass | the Karpenter and Auto Mode facts above, all from labels and spec |

Volume, envFrom and Secret/ConfigMap references are edges, not facts: they do not enter the
fingerprint.

Drift detail = every fact outside the volatile set whose value differs, with both values (a side
without the fact has none).

The key holds group/Kind, so a **kind translation** would show as pending in the source and new in
the target. `compare` pairs two of them after the pairing by id, in the same namespace, with what
is left pending and new:

| Source | Target | Paired by | Facts compared |
|---|---|---|---|
| OpenShift Route | Gateway API HTTPRoute or Ingress | a host of the target is the Route host (the same name first), else the same name | `hosts`, `paths` (no path is `/`), `tls` (Route termination; Ingress `tls`; for an HTTPRoute, an HTTPS or TLS listener on a Gateway it attaches to), `backends` (Service names), `ports` (`service:port`) |
| DeploymentConfig | Deployment | the same name | the pod template facts: images, replicas, requests and limits, service account, PVCs, node selector (not `triggers`, `strategy`, `end_of_life`) |

The pair is one item under the source id, with `target_id` and `translation`
(`"Route→HTTPRoute"`), state `equal` or `drift`, and counts once on each side and as covered in the
namespace coverage. Only the facts both sides express are compared: a fact one side cannot tell
(an HTTPRoute whose Gateway is not in the snapshot, an Ingress collected before 0.2 without
`paths`) is left out. Each target pairs once, sources in id order. `diff` does not translate: in
one cluster, a Route replaced by an HTTPRoute is gone and new. An Ingress replaced by an
HTTPRoute, and objects with generated names (PersistentVolumes `pvc-<uid>`), do not pair.

Per-namespace summary (`_` = cluster-scoped): source total, target total, equal, drift, pending,
new and coverage = (equal + drift) / source total, `null` when the source has nothing there.
Totals and the default table leave system namespaces out, like the default view.

`--format json` prints one document (`schema_version` 1) with `source`, `target`, `totals`,
`namespaces`, `items` (`id`, `ns`, `kind`, `state`, `diff[]`, and `target_id` and `translation`
for a kind translation) plus `index` and `url` of the site:

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

## Diff over time

`kubestrata diff -c <context> [--from <stamp>] [--to <stamp>]` compares two cached snapshots of
the cluster behind a context, by default the newest and the one before it. It reuses
`internal/compare` and the comparison document (`schema_version` 1, `"mode": "diff"`): `source`
is the older snapshot (`--from`), `target` the newer one (`--to`), and the states are:

| State | Meaning |
|---|---|
| equal | in both snapshots, same fingerprint |
| drift | in both, different fingerprint; `diff[]` lists the facts that changed |
| gone | only in the older snapshot |
| new | only in the newer snapshot |

Snapshots of two kubestrata versions (`tool.version` differs) can differ in facts only because one
version collects them (the Auto Mode facts of NodePool, NodeClaim and NodeClass, added in one
release), and the fingerprint covers whatever facts a version collects. So, for `diff` and
`compare` alike, only the facts both sides have decide drift: a fact only one side has goes to the
item's `collector_facts`, and `collector_change` (`from`, `to`, `resources`) at the top of the
document counts the resources that differ only in such facts. The text output prints a note with
the two versions. The cost: a fact a user sets between two snapshots of different versions (a
NodePool `weight`) is listed there, not as drift. With one version, a new fact is drift, as before.

The per-namespace summaries count `equal`, `drift`, `gone` and `new` (no coverage); the totals
leave system namespaces out, like `compare`. The text output lists the namespaces with changes,
then one row per changed resource with the facts as `fact: old → new`, and names the system
namespaces that changed. Stamps are the snapshot file names shown in the `STAMP` column of
`kubestrata cache ls` (`20261120T144809Z`); RFC 3339 times work too. Uses: what an autoscaler or a
job created between two moments (NodeClaims come and go in minutes), what changed in a workload
(image, replicas, requests), and proof of cleanup (no `new` or `gone` left after a test). There is
no site for `diff` yet: the data files are keyed by cluster id (`data/<cluster_id>.js`), and both
snapshots have the same one.

## CLI and TUI

Fang v2 over Cobra for commands; Bubble Tea v2, Bubbles v2, Lip Gloss v2 and Huh for the TUI.
`--format text|json` on every command.

| Command | Does |
|---|---|
| `kubestrata` | TUI: contexts, cache age, status; `r` refresh, `g` generate, `c` compare, `o` open |
| `kubestrata contexts` | kubeconfig contexts (no cluster access) |
| `kubestrata collect [-c ctx]... [--all] [--max-age 6h] [--force] [--snapshot-out file\|dir]` | writes the cache; reuses a snapshot newer than `--max-age`; `--format json` prints a summary per context, `--snapshot-out` also writes the snapshot as plain JSON (`0600`): to a file for one context, as `<cluster_id>.json` into an existing directory for several |
| `kubestrata render [-c ctx] [--scale 30m\|1h\|5d\|10d\|30d\|years\|log\|auto] [--rings 30m,2h,1d,1y] [--axis created\|changed] [-o dir] [--open]` | site from the cache |
| `kubestrata compare -c source -c target [--scale ...] [--rings ...] [--axis ...] [-o dir] [--open]` | coverage table on stdout and side-by-side site from the cache |
| `kubestrata diff -c ctx [--from stamp] [--to stamp]` | two snapshots of one cluster: new / gone / drift / equal (CLI and JSON) |
| `kubestrata cache ls\|prune [--keep 5]` | snapshot housekeeping; `ls` shows the `STAMP` that `diff` takes |

`--redact` (global) masks account ids, endpoint hosts, OpenShift cluster domains and requester team
names in every output (see Redaction).

The page state lives in the URL hash (`#scale=log&axis=changed&color=manager&ns=<namespace>&sel=<id>`),
so a view can be bookmarked and checked headless; `ns=` works in the single-cluster view too. The
search (`q=`) splits terms on commas and spaces (`"quoted text"` is one term): free terms match the
resource text (id, name, kind, app, team, manager, labels, facts) and any of them is enough;
`ns:`, `kind:`, `team:`, `mgr:` and `name:` match the field exactly, `*` as a wildcard, OR within a
field and AND across fields; `-term` excludes; a single term is a substring, as before. While a
search is active the page also writes `data-match` (matching resources, each identity once). A
Content-Security-Policy (`default-src 'none'`) keeps the page off the network. After the first draw
the page writes `data-status`, `data-rings` (ring labels of the first pane), `data-shown`
(resources drawn, summed over the panes), `data-draw-ms` and `data-total-ms` on `<body>` for
automated checks. `make page-check` writes both sites (and renders with custom rings, rings in
years and the auto scale) from the cache, loads them in headless Chrome for several hash states,
including the `30m`, `1h`, `years` and `auto` scales, and fails unless every page reports `data-status="ok"` and, where it checks them, the
expected ring labels. Measured in headless Chrome on an M4: 20 000 resources draw in
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
| F3 | site for the temporal diff (`diff` is in the CLI and JSON already), multi-arch image check, `kubectl strata` via Krew | decided once F2 is in real use |
