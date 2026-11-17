---
type: guide
summary: "Contrato de contexto do repo kubestrata para agentes: CLI Go (MIT) que coleta inventário somente leitura de clusters Kubernetes e gera visão HTML offline por idade (centro = recente) e comparação lado a lado para migração."
updated: 2026-11-17
tags: [agents, kubernetes, migration, inventory]
---

# AGENTS.md — kubestrata

> Contrato de contexto para agentes (LLM) e para o operador.
> Objetivo: sessão fria → produtiva em < 2 min.
> Atualize "Estado atual" ao encerrar qualquer sessão de trabalho relevante.
> Data plane transversal: `~/Tarscase/workspacePessoal/leosavio-org`
> (`knowledge/registry/repos.json`, `knowledge/intent/`, `guide-for-ai.md`).
> Frente no data plane: `leosavio-org/knowledge/work/kubestrata.md`.

## O que é

CLI em Go, MIT, repo `leosavio/kubestrata` (**privado** até a v0.1.0 funcional com docs no padrão
de comunidade; depois público). Lê os contextos do kubeconfig, coleta inventário somente leitura e
sanitizado para um cache local e gera site HTML offline: recursos recentes no centro, antigos na
borda, setor por namespace/time, e comparação lado a lado de dois clusters para planejar migração.
Ferramenta de domínio com repo próprio, como o `costgate`.

## Stack e arquitetura

| Componente | Tecnologia | Nota |
|---|---|---|
| `cmd/kubestrata` | Go 1.27 | versão por `-X main.version` ou `debug.ReadBuildInfo` |
| `internal/cli` | Cobra + Fang v2 (`charm.land/fang/v2`) | `--format text\|json`, JSON com `schema_version` 1 |
| `internal/kube` | client-go v0.37 | kubeconfig (regras padrão, exec plugins); nunca expõe credencial |
| TUI (F1) | Bubble Tea v2, Bubbles v2, Lip Gloss v2, Huh v2 (`charm.land/*`) | a implementar |
| Site (F1) | HTML+CSS+JS inline, zero CDN, canvas 2D, layout polar determinístico | a implementar; modelo de referência: `leosavio-org/knowledge/views/grafo.html` |

Design completo: `docs/design.md`.

## Estado atual (onde parei)

> Última atualização: 2026-11-17

1ª sessão: repo criado (privado), scaffold com `contexts` funcionando (tabela Lip Gloss e JSON),
testes de kubeconfig (inclui teste de não vazamento de token e args de exec), `make check` verde.
Design F1 em `docs/design.md`. Ambiente de teste com dois clusters locais e datas de criação
retroativas (lab `kubito`) em avaliação com o operador.

## Próximos passos

- [ ] F1: `collect` (discovery, list paginado, projeção sanitizada, identidade do cluster) + cache com TTL
- [ ] F1: `render` (site radial, escalas 5d/10d/30d/log, eixo criado/alterado, filtros, busca, painel)
- [ ] F1: `compare` (identidade, fingerprint por kind, tabela de cobertura)
- [ ] F1: TUI básica; teste de não vazamento sobre cache e site; golden de render; e2e em 2 clusters locais
- [ ] v0.1.0: CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, CI, release; depois tornar o repo público

## Comandos essenciais

```sh
make check        # vet + staticcheck (se instalado) + test -race + build
make smoke        # binário contra o kubeconfig local (sem acessar cluster)
```

## Fontes de verdade locais

| Artefato | Caminho |
|---|---|
| Design | docs/design.md |
| ADRs | docs/adr/ (a criar na primeira decisão que mude o design) |
| Tech debt | docs/tech-debt.md (a criar no primeiro débito) |

## Guardrails

- Somente leitura no cluster: verbos `list` e `get`. Nada de `create`, `patch`, `delete`, `exec`.
- Valor de Secret, `env[].value`, `args`, `command` e a annotation
  `kubectl.kubernetes.io/last-applied-configuration` nunca entram no cache nem no site.
- Credencial do kubeconfig (token, certificado, args de exec) nunca aparece em saída.
- Cache e site fora de git (`0700`/`0600` no diretório de cache do usuário); `.gitignore` cobre
  `/site/` e `*.snapshot.json*`.
- `render`/`compare` não acessam cluster e não dependem de relógio: referência = `collected_at`.
- Sem telemetria no binário.
- Docs públicas em inglês com `README.pt-BR.md`; AGENTS.md e ADRs em PT-BR.
- Licença: MIT (`LICENSE`). Entrega a usuários segue `leosavio-org/knowledge/templates/licencas-e-creditos.md`.
