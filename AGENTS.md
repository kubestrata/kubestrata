---
type: guide
summary: "Contrato de contexto do repo kubestrata para agentes: CLI Go (MIT) que coleta inventário somente leitura de clusters Kubernetes e gera visão HTML offline por idade (centro = recente) e comparação lado a lado para migração."
updated: 2026-11-18
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
| `internal/kube` | client-go v0.37 | kubeconfig (regras padrão, exec plugins); `ClusterSource` só list/get; Secrets via Table |
| `internal/collect` | projeção por kind | `Source` (interface) testável sem cluster; agregação, arestas, fingerprint, distro |
| `internal/snapshot` | contrato JSON `schema_version` 1 | gzip; recusa versão maior |
| `internal/cache` | `~/Library/Caches/kubestrata` (`KUBESTRATA_CACHE_DIR`) | `clusters/<id>/<stamp>.json.gz` + `contexts.json`; escrita atômica |
| `internal/compare` | Go puro, determinístico | pares por `group/Kind/ns/name`: equal, drift (fatos fora do `volatile` de `collect.go`), pending, new; resumo e cobertura por namespace |
| TUI (F1) | Bubble Tea v2, Bubbles v2, Lip Gloss v2, Huh v2 (`charm.land/*`) | a implementar |
| Site (F1) | HTML+CSS+JS inline (`web/`), zero CDN, canvas 2D, layout polar determinístico | `render` (um cluster) e `compare` (dois painéis, modo de cor `state`, tabela de cobertura); modelo de referência: `leosavio-org/knowledge/views/grafo.html` |
| CI | GitHub Actions `.github/workflows/ci.yml` | lint (gofmt, vet incl. tag `lab`, staticcheck v0.8.2, cross-compile darwin/linux × arm64/amd64) e `go test -race`; sem job de lab (o e2e precisa do lab `kuberito`) |

Design completo: `docs/design.md`.

## Estado atual (onde parei)

> Última atualização: 2026-11-18

Sessão 2026-11-18 (workflow `wf_6f330db99bdf414e`, mudanças sem commit para revisão do
orquestrador): `compare` pronto. `internal/compare` classifica por id (`equal`/`drift`/`pending`/
`new`), lista os fatos que diferem no drift e resume por namespace (cobertura = (equal + drift) /
origem; totais sem namespaces de sistema). `render.Write` ganhou o modo `compare` (`data/compare.js`
+ `json/compare.json`); a página mostra dois painéis com setores comuns, cor `state`, destaque do
par nos dois lados, painel com os dois lados e tabela de cobertura clicável. No lab: 320 recursos na
origem, 147 no destino, cobertura 42% (90 equal, 44 drift, 186 pending, 13 new); `payments/api` drift
de imagem 2.56 → 2.57, `catalog/search` drift de réplicas, `legacy-portal` e `data` 100% pending.
Achado: HTTPRoute que substitui Route/Ingress aparece como pending + new (a chave inclui o kind).
CI (`ci.yml`) espelhando o do `kuberito`, sem job de lab. README com badges, início rápido, lab
`kuberito` e duas telas em `docs/images/`. Correções da revisão (workflow `wf_2e2da1fff3c31361`):
clique na tabela de cobertura liga o filtro `ns=<namespace>` (igualdade com o namespace, nos dois
painéis e nos setores, chip no cabeçalho para limpar; a busca `q` continua independente); CLI
arredonda com `math.Round` como o `Math.round` da página; rodapé rotula as bases (identidade contada
uma vez, estados com os filtros ativos, cobertura sem sistema); `make page-check` no repo
(`scripts/page-check.sh`); início rápido com `git clone` + `make build` + `export PATH`.

1ª sessão (2026-11-17): repo criado (privado); `contexts`, `collect` e `cache ls|prune`
funcionando. `collect` usa discovery, list paginado, Secrets pela Table do servidor (só metadata,
linha que não for `PartialObjectMetadata` é recusada), projeção sanitizada, agregação de
pods/RS/RC/Jobs no dono, arestas e fingerprint. Testes no fixture `internal/collect/testdata/cluster.yaml`
(Helm, OpenShift simulado, operator, órfãos, valores `LEAK-` semeados) e smoke real no cluster do
OrbStack (71 recursos, 0 erros, cache `0700`/`0600`). Design EN + PT-BR.

`render` pronto: `web/` (HTML, CSS, JS embutidos por `go:embed`), `internal/render` grava
`index.html` + `data/<id>.js` + `json/<id>.json`, determinístico, CSP sem rede. Conferido no Chrome
headless contra o lab `kuberito` (320 recursos, sem erro de JS) e com 20 000 recursos sintéticos
(desenho em 10–16 ms). E2e `make lab-test` (`test/e2e`, tag `lab`) compara a coleta com a
verdade-base do lab: 0 divergência, 0 vazamento.

## Próximos passos

- [x] F1: `collect` (discovery, list paginado, projeção sanitizada, identidade do cluster) + cache com TTL
- [x] F1: `render` (site radial, escalas 5d/10d/30d/log/custom, eixo criado/alterado, filtros, busca, painel, cor por categoria/manager/time)
- [x] F1: `compare` (identidade, fingerprint, drift com fatos, tabela de cobertura, site lado a lado; e2e no lab)
- [x] CI: `.github/workflows/ci.yml` (lint + test -race); primeira execução verde em 2026-11-18 (run 44340222456, `04f7c74`)
- [ ] F1: TUI básica; teste de não vazamento sobre o site; golden de render
- [ ] F2: parear tradução de kind (Route/Ingress → HTTPRoute) e PVs de nome gerado nas unidades de migração
- [ ] v0.1.0: CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, release; depois tornar o repo público

## Comandos essenciais

```sh
make check        # gofmt + vet + staticcheck (se instalado) + test -race + build
make fmt-fix      # gofmt -w
make smoke        # binário contra o kubeconfig local (sem acessar cluster)
make lab-test     # e2e contra o lab kuberito rodando (pula se não houver lab)
make page-check   # render + compare do cache, Chrome headless em vários estados de hash; falha sem data-status="ok"
                  # (KUBESTRATA_CACHE_DIR; PAGE_CHECK_SOURCE/PAGE_CHECK_TARGET, padrão kuberito-source/kuberito-target)
bin/kubestrata render -c <ctx> --open   # site offline a partir do cache
bin/kubestrata compare -c <origem> -c <destino> --open   # tabela de cobertura + site lado a lado
KUBESTRATA_CACHE_DIR=/tmp/ks bin/kubestrata collect -c <ctx> --force   # coleta real, cache isolado
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
- Idioma (operador, 2026-11-17, vale para `kubestrata` e `kuberito`): inglês é o principal em
  código, identificadores, contextos, nomes de cenário e docs; cada doc pública tem a tradução em
  arquivo separado `*.pt-BR.md` (ex.: `docs/design.md` + `docs/design.pt-BR.md`). AGENTS.md e
  ADRs em PT-BR (regra do portfólio, evolution-log entrada 7).
- Licença: MIT (`LICENSE`). Entrega a usuários segue `leosavio-org/knowledge/templates/licencas-e-creditos.md`.
