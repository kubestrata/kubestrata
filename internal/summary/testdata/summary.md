# kubestrata summary: source

Cluster `0123456789ab`, openshift v1.37.0, collected 2026-11-17T12:00:00Z by kubestrata test. 9 resources outside system namespaces (1 more in system namespaces, left out below).

## Categories

| Category | Resources |
|---|---|
| workload | 4 |
| network | 3 |
| platform | 1 |
| storage | 1 |

## Kinds

| Kind | Resources |
|---|---|
| apps.openshift.io/DeploymentConfig | 1 |
| apps/Deployment | 1 |
| apps/ReplicaSet | 1 |
| core/Pod | 1 |
| core/Service | 1 |
| machineconfiguration.openshift.io/MachineConfig | 1 |
| networking.k8s.io/Ingress | 1 |
| route.openshift.io/Route | 1 |
| storage.k8s.io/StorageClass | 1 |

## Nodes

Nodes: 3; architectures: amd64: 3; instance types: m6a.large: 3; zones: zone-a: 2, zone-b: 1.

## Managers

| Manager | Resources | Migration path |
|---|---|---|
| helm | 3 | reinstall the release in the target |
| kubectl | 2 | applied by hand: export, clean, move to Git |
| platform | 2 | recreated by the target platform, do not migrate |
| console | 1 | edited in a web console: review and codify before migrating |
| other | 1 | unknown tool: check managedFields |

## Top namespaces (by resources, up to 10)

| Namespace | Team | Resources |
|---|---|---|
| ns-a | team-a | 4 |
| ns-b |  | 3 |

## Deprecated or end of life

| Kind | Resources | Note |
|---|---|---|
| apps.openshift.io/DeploymentConfig | 1 | DeploymentConfig is deprecated since OpenShift 4.14; target: Deployment |
| networking.k8s.io/Ingress | 1 | ingress-nginx was retired in March 2026; target: Gateway API |

## Without an expected owner

| Resource | Why |
|---|---|
| apps/ReplicaSet/ns-b/old-7f9c | owner not collected |
| core/Pod/ns-b/debug | no owner |

## Teams

1 of 2 namespaces have a team; 4 of 7 of their resources (57%).

## Argo CD

Argo CD installed, no managed objects found (2 Applications, 0 deploying to other clusters).

## Collection

Collection errors: 1; kinds left out on purpose: core/Secret.
