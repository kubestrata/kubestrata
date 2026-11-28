---
type: guide
summary: "Contrato de contexto do repo kubestrata para agentes: CLI Go (MIT) que coleta inventário somente leitura de clusters Kubernetes e gera visão HTML offline por idade (centro = recente) e comparação lado a lado para migração."
updated: 2026-11-27
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

CLI em Go, MIT, repo `kubestrata/kubestrata` (público desde 2026-11-20; org própria e v0.1.0 desde
2026-11-26). Lê os contextos do kubeconfig, coleta inventário somente leitura e
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
| TUI | — | não existe; foi do plano da 0.1 (F1) para a F3 do `docs/design.md` |
| Site (F1) | HTML+CSS+JS inline (`web/`), zero CDN, canvas 2D, layout polar determinístico | `render` (um cluster) e `compare` (dois painéis, modo de cor `state`, tabela de cobertura); modelo de referência: `leosavio-org/knowledge/views/grafo.html` |
| CI | GitHub Actions `.github/workflows/ci.yml` + `lab.yml` | lint (gofmt, vet incl. tag `lab`, staticcheck v0.8.2, cross-compile darwin/linux × arm64/amd64) e `go test -race`; `lab.yml` instala o kuberito da release (versão fixada, checksum + cosign), `kuberito up --profile full`, `make lab-test` e `make page-check`, só com a variável `KUBERITO_PUBLIC=true` (o kuberito é público) |
| Release | `.github/workflows/release.yml` (tag `v*`) | testes, binários darwin/linux × arm64/amd64, smoke do linux/amd64, `checksums.txt` assinado com cosign keyless, release no GitHub e `Formula/kubestrata.rb` no `kubestrata/homebrew-tap` (deploy key só de escrita no tap, secret `HOMEBREW_TAP_DEPLOY_KEY`; pré-release não mexe no tap) |
| Smoke de instalação | `.github/workflows/install-smoke.yml` + `scripts/smoke-check.sh` | brew, `go install` e tarball assinado em 4 runners; disparado pelo último job do `release.yml` (`workflow_dispatch`), toda terça e à mão |
| PyPI | job `pypi` do `release.yml` + `scripts/build-wheels.py` | wheel por plataforma com o binário do archive assinado (cosign e checksum conferidos antes); Trusted Publishing da conta `leosavio`, environments `testpypi` (tag com `-`) e `pypi` (tag final), só tags `v*` |
| Segurança | `.github/workflows/security.yml`, `.github/dependabot.yml`, settings do repo | `govulncheck` v1.10.0 (falha com vulnerabilidade alcançável; semanal) e `zizmor` 1.33.1 nos workflows; Dependabot (gomod, actions), alertas e security updates; secret scanning + push protection; CodeQL default setup |

Design completo: `docs/design.md`.

## Estado atual (onde parei)

> Última atualização: 2026-11-27 (teste de campo no OpenShift e revisão independente, K12–K30)

Sessão 2026-11-27 (branch `feat/openshift-field-test`, um commit por item, sem push): K12
`--redact` para OpenShift (`api.redacted`, `*.apps.redacted`, host do `oc login`, times do
`openshift.io/requester` como `redacted-N`; `namespaces[].team_from`); K13 escalas `years`, `auto`
e sufixo `y` (o padrão continua `30d`: no lab o `auto` espreme as ondas do destino); K14 Argo CD
pelo `status.resources` das Applications e `cluster.argocd`; K20 `--skip-kinds`/`--no-secrets` e
`deploy/rbac/` (testado no `kuberito-source` com token de ServiceAccount, RBAC apagado); K23 busca
com vários valores; K25 `compare` pareia Route→HTTPRoute/Ingress e DeploymentConfig→Deployment
(`target_id`, `translation`; no lab `auth/sso` vira drift de hosts); K30 `lab.yml` (pulado até
`KUBERITO_PUBLIC=true`); K15 managers `kapp` e `console`; K16 CSVs copiados fundidos e categoria
`platform`; K17 `--team-from-ns`, requester admin ignorado, cobertura de times; K21
`--show-system`; K24 SBOM CycloneDX e attestations no `release.yml` (não exercitado); K18
`kubestrata summary`; K19 higiene do kubeconfig no `contexts`; K22 `docs/openshift.md`; K26
cobertura no CI e Scorecard; K27 `schemas/` (conferidos contra saídas reais) e
`docs/compatibility.md`; K28 docs alinhadas (TUI foi para a F3); K29 `docs/roadmap.md`. Falta:
release 0.2.0 (exercita SBOM e attestations), piloto OpenShift → EKS à mão antes da
`1.0.0-rc.1`, decisões abertas no relatório da sessão (Ingress→HTTPRoute, totais do compare sem
kinds `platform`, usuário do nome `oc login`, `actions/attest`).

Sessão 2026-11-26: repo transferido para a org `kubestrata` (PR #8, caminho do módulo
`github.com/kubestrata/kubestrata`). Release v0.1.0 no mesmo padrão do `costgate`: `release.yml`,
`install-smoke.yml`, `scripts/homebrew-formula.sh` e `scripts/smoke-check.sh`; README com seção
Install (brew, `go install`, tarball verificado) e plataformas testadas; CHANGELOG 0.1.0. O
install-smoke da 0.1.0 pegou o `go install` imprimindo `v0.1.0` no `--version`; a 0.1.1 tira o `v`.
A 0.1.2 muda a fórmula para o tap da org (`kubestrata/homebrew-tap`, `brew install
kubestrata/tap/kubestrata`); o `leosavio/tap` ficou com `tap_migrations.json` apontando para cá.
A 0.1.3 é de segurança: Go 1.27.1 → 1.27.4 (11 vulnerabilidades da stdlib alcançáveis pelo
`govulncheck`) e `golang.org/x/net` v0.62.0, mais `security.yml`, Dependabot e o install-smoke sem
`workflow_run`/`GITHUB_ENV` (zizmor 0 achados).
Distribuição por PyPI/uv em avaliação (decisão aberta no workstream do leosavio-org).

Validação 2 no workshop Spark on EKS (`aws-spark-on-eks-workshop/extra-02-avaliacao-ferramentas/kubestrata-avaliacao.md`,
build `ca851b5`): K1a–K1c, K3, K4 e K6 ok; o `diff` bate com o `snapdiff.sh` do workshop. K10: o
`diff` e o `compare` entre snapshots de versões diferentes do coletor só contam como drift os fatos
que os dois lados têm (`collector_facts` por item, `collector_change` no documento, nota no texto).
K9: `collect --snapshot-out` sem `--redact` avisa no stderr. K11 (duas coletas `--force` iguais)
ficou como está. `make check`, `make page-check` (20/20) e `make lab-test` verdes.

Sessão 2026-11-20 (workflow `wf_fd5a30f0a91e7204`, lane 1, mudanças sem commit para revisão do
orquestrador): achados da avaliação no workshop AWS (EKS Auto Mode + Spark + Terraform,
relatório de avaliação K1–K8 no repo privado de notas do workshop). Escalas `30m`
e `1h` (página e `--scale`), rótulos e idades abaixo de um dia em minutos/horas, `--rings` com
sufixos `m`/`h`/`d` (config da página: `ring_minutes`), `data-rings` no `<body>` conferido pelo
`make page-check` (20 páginas). K6: padrão de sistema por nome exato (`kube-prometheus-stack` deixa
de ser sistema). K3: managers `terraform` (`HashiCorp`, `Terraform`, `terraform-provider-*`) e
`platform` (`eks*`, `kubeadm`, `cluster-version-operator`, control plane quando único escritor);
empate no mesmo segundo vai para quem não é plataforma; `manager` e `field_managers` fora do `fp`
(teste). K8: `collect --snapshot-out <arquivo|dir>`. K5: `--redact` global (pacote
`internal/redact`; cache intacto; erros de todo comando mascarados em `NewRoot`). K1: `kubestrata diff` (CLI/JSON, `compare.CompareTimeline`,
estados new/gone/drift/equal; sem site porque `data/<cluster_id>.js` colide) e coluna `STAMP` no
`cache ls`. K2: fixture SparkApplication → driver → executores confirma a agregação sem mudar
código; confirmado também no cluster EKS do workshop (Lab 3: SparkApplication com `pods` 5,
`ready` 5, `archs` `[arm64]` durante o job e `pods` 1 depois). K4: fatos de
NodeClaim/NodePool/NodeClass/EC2NodeClass (labels e spec) e contagem de instance/capacity type no
resumo de nodes. No lab: `make lab-test` verde, `diff` entre duas coletas seguidas = 0 mudanças.
Passada de correção da revisão: `--redact` mascara também a mensagem de erro de todo comando
(`maskErrors` em `NewRoot`), regex de endpoint exige dois labels antes de `eks.amazonaws.com` /
`azmk8s.io`, e o início rápido do README põe `$(go env GOPATH)/bin` no `PATH` (achado D32 do
workshop). Snapshots têm granularidade de segundo: duas coletas no mesmo segundo gravam um só.

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
- [x] Achados do workshop AWS: escalas 30m/1h, K1 `diff` (CLI/JSON), K2 verificado (fixture + workshop), K3, K4, K5 `--redact`, K6, K8 `--snapshot-out`
- [ ] K1: site do `diff` (exige mudar a chave dos arquivos `data/<cluster_id>.js`); K7 custo por recurso a partir de export do Kubecost/OpenCost (fora desta rodada)
- [x] Teste de campo no OpenShift e revisão independente: K12–K30 (2026-11-27, branch `feat/openshift-field-test`)
- [ ] Release 0.2.0; piloto OpenShift → EKS conferido à mão; `1.0.0-rc.1` (`docs/roadmap.md`)
- [ ] F3: TUI interativa (saiu do plano da F1); teste de não vazamento sobre o site; golden de render
- [ ] F2: Ingress → HTTPRoute no `compare` (decisão aberta), `--map-ns` e PVs de nome gerado nas unidades de migração
- [x] CONTRIBUTING, CODE_OF_CONDUCT, SECURITY e CHANGELOG (`e8c436f`); repo público desde 2026-11-20
- [x] v0.1.0: release (2026-11-26), Homebrew `kubestrata/tap/kubestrata`, `go install`, tarball assinado

## Comandos essenciais

```sh
make check        # gofmt + vet + staticcheck (se instalado) + test -race + build
make fmt-fix      # gofmt -w
make smoke        # binário contra o kubeconfig local (sem acessar cluster)
make lab-test     # e2e contra o lab kuberito rodando (pula se não houver lab)
make page-check   # render + compare do cache, Chrome headless em vários estados de hash; falha sem data-status="ok"
                  # (KUBESTRATA_CACHE_DIR; PAGE_CHECK_SOURCE/PAGE_CHECK_TARGET, padrão kuberito-source/kuberito-target)
bin/kubestrata render -c <ctx> --open   # site offline a partir do cache (--scale 30m|1h|5d|10d|30d|log, --rings 30m,2h,1d)
bin/kubestrata compare -c <origem> -c <destino> --open   # tabela de cobertura + site lado a lado
bin/kubestrata diff -c <ctx> [--from <stamp>] [--to <stamp>]   # dois snapshots do mesmo cluster (stamps: cache ls)
bin/kubestrata collect -c <ctx> --snapshot-out snap.json --redact   # snapshot em JSON puro, mascarado para compartilhar
bin/kubestrata summary -c <ctx> [--format json]   # resumo de migração do snapshot mais novo
go test ./schemas                                  # saídas JSON reais contra os JSON Schemas
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
