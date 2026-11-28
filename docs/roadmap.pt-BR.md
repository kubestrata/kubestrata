# Roadmap

[English](roadmap.md)

O kubestrata está na 0.1.x. A próxima release, 0.2.0, traz o trabalho da primeira execução num
OpenShift e de uma revisão independente do projeto. Uma `1.0.0-rc.1` vem depois que uma comparação
OpenShift → EKS for conferida à mão numa migração real, e então a 1.0.0.

## Checklist da 1.0

| Item | Estado | Onde |
|---|---|---|
| JSON Schemas para toda saída JSON, conferidos contra saídas reais nos testes | feito | [`schemas/`](../schemas/), `go test ./schemas`, o e2e do lab |
| Política de compatibilidade escrita: o que o `schema_version` 1 cobre, o que pode mudar antes da 1.0, as regras a partir da 1.0 | feito; as regras da 1.0 são uma proposta a adotar com a 1.0 | [compatibility.pt-BR.md](compatibility.pt-BR.md) |
| RBAC só de leitura publicado, com e sem Secrets | feito | [`deploy/rbac/`](../deploy/rbac/), README "Permissões" |
| SBOM e build provenance SLSA para os pacotes da release | feito: a partir da 0.2.0, conferido com `gh attestation verify` | [security-analysis.pt-BR.md](security-analysis.pt-BR.md) |
| Testes ponta a ponta no CI sobre o lab do kuberito | feito no `lab.yml`: todo pull request, todo push na `main` e toda semana, com o kuberito v0.2.0 | [`lab.yml`](../.github/workflows/lab.yml) |
| OpenSSF Scorecard e cobertura de testes medida | feito: Scorecard semanal e na `main`; cobertura impressa no CI, sem limite mínimo ainda | [`scorecard.yml`](../.github/workflows/scorecard.yml), [`ci.yml`](../.github/workflows/ci.yml) |
| Piloto de comparação OpenShift → EKS conferido à mão: traduções de kind (Route → HTTPRoute ou Ingress, DeploymentConfig → Deployment), cobertura e drift numa migração real | não iniciado | condição para a `1.0.0-rc.1` |
| Um segundo revisor para as mudanças | não planejado: risco aceito | ver abaixo |

### Mantenedor único

O kubestrata tem um mantenedor e nenhum segundo revisor. É um risco aceito por enquanto (decisão
do mantenedor): toda mudança roda as verificações de [security-analysis.pt-BR.md](security-analysis.pt-BR.md),
as actions são fixadas, as releases são assinadas e com attestation, e o código é pequeno. O risco
é revisto se o projeto ganhar contribuidores regulares ou usuários corporativos.

## Depois da 1.0

Das fases do design ([design.pt-BR.md](design.pt-BR.md), "Fases"):

| Fase | Escopo |
|---|---|
| F2 | unidades de migração (pareamento entre namespaces com `--map-ns`, PersistentVolumes com nome gerado), classificação (exposição, acoplamento à nuvem), `plan --format json\|csv\|md`, um resumo por time |
| F3 | um site para o `diff`, checagem de imagem multi-arch, `kubectl strata` via Krew, uma TUI interativa (planejada para a 0.1 no começo, não construída) |

Questões em aberto, sem data: parear no `compare` um Ingress com a HTTPRoute que o substitui, o
custo por recurso a partir de um export do Kubecost ou do OpenCost.
