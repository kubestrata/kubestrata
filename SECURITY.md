# Security

## Scope

kubestrata reads clusters with `list` and `get` only and writes nothing to them.

| Area | What kubestrata does |
|---|---|
| Cluster access | the credentials of your kubeconfig, including exec plugins (`aws eks get-token`, `oc`); none are stored or printed |
| Secrets | read as metadata only, through the server-side Table (`includeObject=Metadata`): type and key count, never values |
| Sanitization | the snapshot drops `last-applied-configuration`, env values, args, commands, Route TLS keys and cloud identity ids |
| Local files | cache and site under the user cache dir with modes `0700` / `0600`; never sent anywhere; no telemetry |
| HTML page | offline, Content-Security-Policy without network access; cluster strings rendered as text only |

The cache and the generated site describe your cluster (namespaces, images, hosts). Treat them as
you treat the cluster's inventory: do not commit or publish them.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting on this repository (**Security** → **Report a
vulnerability**). Do not open a public issue for a security problem. You will get an answer within
7 days.
