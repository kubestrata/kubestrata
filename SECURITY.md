# Security

## Scope

kubestrata reads clusters with `list` and `get` only and writes nothing to them.

| Area | What kubestrata does |
|---|---|
| Cluster access | the credentials of your kubeconfig, including exec plugins (`aws eks get-token`, `oc`); none are stored or printed. Read-only ClusterRoles to collect with a ServiceAccount token: [`deploy/rbac/`](deploy/rbac/) (README, "Permissions") |
| Secrets | read as metadata only, through the server-side Table (`includeObject=Metadata`): type and key count, never values. RBAC cannot grant metadata only: `list` on Secrets exposes their values to whoever holds it, so `collect --no-secrets` with `kubestrata-reader-no-secrets` avoids that permission |
| Sanitization | the snapshot drops `last-applied-configuration`, env values, args, commands, Route TLS keys and cloud identity ids |
| Local files | cache and site under the user cache dir with modes `0700` / `0600`; never sent anywhere; no telemetry |
| HTML page | offline, Content-Security-Policy without network access; cluster strings rendered as text only |

`kubestrata contexts` flags contexts that skip TLS verification or hold a static token (README,
"Kubeconfig hygiene"): fix those before collecting, and log out (`oc logout`) when done.

The cache and the generated site describe your cluster (namespaces, images, hosts). Treat them as
you treat the cluster's inventory: do not commit or publish them.

`--redact` (global flag) masks the AWS account id in ARNs and ECR registry hosts, the cluster label
of EKS and AKS endpoint hosts, the `<cluster>.<base domain>` part of OpenShift API hosts
(`api.<cluster>.<base>`, `api-int.<cluster>.<base>`) and of every host under
`apps.<cluster>.<base>` (Routes, Ingresses, error messages), the dashed API host that `oc login`
writes into context, cluster and user names, any 12-digit run in those names, and team names taken
from the `openshift.io/requester` annotation, which are user names (`redacted-1`, `redacted-2`,
the same mask for the same user in one run). It applies to the output and error messages of
`contexts`, `collect` (and its `--snapshot-out` files), `cache ls`, `diff`, `render` and `compare`,
and to the generated site; the cache keeps the real values. Use it for screenshots, shared
terminals and files you hand over. It does not anonymize namespaces, resource names, images
outside ECR, labels, custom domains (`shop.example.com`) or the user part of `oc login` names:
review a site before publishing it. Details: [docs/design.md](docs/design.md#redaction---redact).

## Supported versions

Fixes go to the latest release only. Versions 0.1.0 to 0.1.2 were built with Go 1.27.1 and carry
standard-library vulnerabilities fixed in 0.1.3.

## Automated checks

`govulncheck`, `zizmor`, CodeQL, Dependabot and secret scanning run on every change and weekly. The
list of checks, the findings so far and how to repeat them on a release are in
[docs/security-analysis.md](docs/security-analysis.md).

## Reporting a vulnerability

Use GitHub's private vulnerability reporting on this repository (**Security** → **Report a
vulnerability**). Do not open a public issue for a security problem. You will get an answer within
7 days.
