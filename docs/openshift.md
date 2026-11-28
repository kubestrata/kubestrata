# First run on OpenShift

[Português](openshift.pt-BR.md)

This guide takes kubestrata through a first inventory of an OpenShift 4 cluster, and then a
comparison with the cluster you migrate to. Every example uses placeholders: `ocp` for the
OpenShift context, `eks` for the target, `api.<cluster>.<base-domain>` for the API host.

kubestrata reads the cluster with `list` and `get` only and keeps what it collects in a local
cache (`~/Library/Caches/kubestrata` on macOS, `~/.cache/kubestrata` on Linux). Nothing is sent
anywhere.

## 1. Log in and check the context

Log in with the cluster's CA instead of `--insecure-skip-tls-verify`. The CA bundle of the API
server's serving certificate comes from the cluster administrator; check its fingerprint out of
band.

```sh
oc login --certificate-authority=ca.crt https://api.<cluster>.<base-domain>:6443
kubestrata contexts
```

`oc login` names the context `<namespace>/api-<cluster>-<base-domain>:6443/<user>`. Rename it to
something short, since kubestrata keys its cache by context name:

```sh
kubectl config rename-context "$(kubectl config current-context)" ocp
```

The `NOTES` column of `kubestrata contexts` flags a cluster that skips TLS verification and a user
with a static token. An `oc login` token is an OAuth access token: it is static for the
kubeconfig, so log out when you are done (step 8). README, "Kubeconfig hygiene", has the details.

## 2. Choose the permissions

A cluster administrator can create a read-only ServiceAccount for the inventory, so that the
collection does not run with a personal token:

| File | For |
|---|---|
| [`deploy/rbac/kubestrata-reader.yaml`](../deploy/rbac/kubestrata-reader.yaml) | `list` on every kind, Secrets included (as metadata) |
| [`deploy/rbac/kubestrata-reader-no-secrets.yaml`](../deploy/rbac/kubestrata-reader-no-secrets.yaml) | the inventory kinds, the OpenShift API groups and common custom resources, never Secrets: collect with `--no-secrets` |

Without Secrets the inventory loses the Helm release state and the links from workloads to
Secrets; Helm is still detected from object labels. README, "Permissions", shows how to build a
kubeconfig with a short-lived token of the ServiceAccount.

## 3. Collect

```sh
kubestrata collect -c ocp --no-secrets          # or without --no-secrets with kubestrata-reader
kubestrata collect -c ocp --no-secrets --team-from-ns '^([a-z]+)-(dev|test|prod)$'
```

The `TEAMS` column shows how many namespaces have a team. kubestrata takes a namespace's team
from its labels and annotations (`team`, `owner`, `app.kubernetes.io/part-of`), then from its name
when you pass `--team-from-ns` (the first group of the regexp names the team), then from
`openshift.io/requester`, the user who requested the project, unless that user is `kube:admin`,
`system:*`, `admin` or one of `--requester-ignore`. A requester is a user name: treat it as
personal data.

Namespaces matching `openshift-*`, `kube-*` and the cloud provider prefixes are system namespaces:
they are collected, hidden by default in the page and left out of the totals.

## 4. Look at it

A long-lived cluster keeps most of its resources older than a year. The default `30d` scale puts
them in its outer band; use rings in years, or `auto` (rings at quantiles of the ages):

```sh
kubestrata render -c ocp --scale years --open
kubestrata render -c ocp --rings 30d,90d,180d,1y,2y,3y,5y --open
kubestrata render -c ocp --scale auto --open
```

What the page shows for OpenShift:

| You see | Where |
|---|---|
| Routes | network category, with host, path, TLS termination and backend Service in the detail panel |
| DeploymentConfigs | workload category, with their ReplicationControllers and pods folded in, and `end_of_life`: deprecated since OpenShift 4.14, target Deployment |
| ingress-nginx Ingresses, in-tree StorageClasses | `end_of_life` with their replacement |
| MachineConfig, MachineConfigPool, APIRequestCount, cluster operator configs, the Machine API | the `platform` category, hidden by default: click its chip or add `show=platform` to the URL |
| the ClusterServiceVersions OLM copies into every namespace | folded into the original, which counts them (`copies`) |
| Argo CD (OpenShift GitOps) | manager `argocd`, from its labels and annotations or from the Applications' `status.resources`; the overview says when Argo CD is installed but manages nothing collected |
| edits made in the web console | manager `console`: a manual change to codify before migrating |
| Carvel kapp | manager `kapp` |

The search box takes several values: `ns:payments,ns:checkout kind:DeploymentConfig`,
`-mgr:platform`.

For the numbers a migration plan starts from, as Markdown:

```sh
kubestrata summary -c ocp > ocp-summary.md
```

## 5. Share without leaking the cluster's identity

`--redact` masks, in every output, the file of `--snapshot-out` and the site: the
`<cluster>.<base-domain>` part of `api.`, `api-int.` and `*.apps.` hosts (`api.redacted:6443`,
`web.apps.redacted`), the dashed API host in `oc login` context names, and the team names taken
from `openshift.io/requester` (`redacted-1`, `redacted-2`). It keeps namespaces, resource names,
images and custom Route domains: review a site before you publish it.

```sh
kubestrata render -c ocp --redact -o ./ocp-site
kubestrata collect -c ocp --snapshot-out ocp.json --redact
```

## 6. Compare with the target

Collect the target with its own context, then compare:

```sh
kubestrata collect -c eks
kubestrata compare -c ocp -c eks --open
```

`compare` pairs resources by `group/Kind/namespace/name`, then pairs the OpenShift kinds with what
the target serves instead, in the same namespace: a Route with an HTTPRoute or an Ingress (a host of
the target is the Route host, the same name first, else the same name) and a DeploymentConfig with
the Deployment of the same name. Those pairs are equal or drift on the facts both kinds express
(host, path, TLS, backend Service and port; images, replicas and the rest of the pod template),
count as covered, and show the translation (`Route→HTTPRoute`) in the detail panel. A namespace
renamed in the target does not pair yet.

## 7. Follow a cluster over time

```sh
kubestrata collect -c ocp --force      # again, later
kubestrata diff -c ocp                 # what appeared, disappeared or changed
```

## 8. Clean up

```sh
kubectl delete -f deploy/rbac/kubestrata-reader-no-secrets.yaml   # if you created the ServiceAccount
oc logout                                                       # deletes the OAuth token on the server
kubestrata cache ls                                             # the snapshots you keep
kubestrata cache prune --keep 1
```

The cache and the sites describe the cluster: keep them out of git and of shared drives.
