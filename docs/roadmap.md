# Roadmap

[Português](roadmap.pt-BR.md)

kubestrata is at 0.1.x. The next release, 0.2.0, carries the work from the first run on OpenShift
and from an independent review of the project. A `1.0.0-rc.1` follows once an OpenShift → EKS
comparison has been checked by hand on a real migration, then 1.0.0.

## 1.0 checklist

| Item | State | Where |
|---|---|---|
| JSON Schemas for every JSON output, checked against real outputs in the tests | done | [`schemas/`](../schemas/), `go test ./schemas`, the lab e2e |
| Written compatibility policy: what `schema_version` 1 covers, what may change before 1.0, the rules from 1.0 on | done; the 1.0 rules are a proposal to adopt with 1.0 | [compatibility.md](compatibility.md) |
| Published read-only RBAC, with and without Secrets | done | [`deploy/rbac/`](../deploy/rbac/), README "Permissions" |
| SBOM and SLSA build provenance for the release archives | done in `release.yml`, not exercised yet: 0.2.0 is the first release with them | [security-analysis.md](security-analysis.md) |
| End-to-end tests in CI on the kuberito lab | done in `lab.yml`: every pull request, every push to `main` and weekly, on kuberito v0.2.0 | [`lab.yml`](../.github/workflows/lab.yml) |
| OpenSSF Scorecard and measured test coverage | done: Scorecard weekly and on `main`; coverage printed in CI, no threshold yet | [`scorecard.yml`](../.github/workflows/scorecard.yml), [`ci.yml`](../.github/workflows/ci.yml) |
| OpenShift → EKS comparison pilot checked by hand: kind translations (Route → HTTPRoute or Ingress, DeploymentConfig → Deployment), coverage and drift on a real migration | not started | gate for `1.0.0-rc.1` |
| A second reviewer for changes | not planned: accepted risk | see below |

### Single maintainer

kubestrata has one maintainer and no second reviewer. That is an accepted risk for now (maintainer
decision): every change runs the checks of [security-analysis.md](security-analysis.md), actions are
pinned, releases are signed and attested, and the code is small. The risk is revisited if the
project gets regular contributors or enterprise users.

## After 1.0

From the design's phases ([design.md](design.md), "Phases"):

| Phase | Scope |
|---|---|
| F2 | migration units (pairing across namespaces with `--map-ns`, PersistentVolumes with generated names), classification (exposure, cloud coupling), `plan --format json\|csv\|md`, a brief per team |
| F3 | a site for `diff`, a multi-arch image check, `kubectl strata` via Krew, an interactive TUI (planned for 0.1 at first, not built) |

Open questions, without a date: pairing an Ingress with the HTTPRoute that replaces it in
`compare`, the cost per resource from a Kubecost or OpenCost export.
