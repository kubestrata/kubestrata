# kubestrata

[![CI](https://github.com/leosavio/kubestrata/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/leosavio/kubestrata/actions/workflows/ci.yml?query=branch%3Amain)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

[English](README.md)

> **Status: pré-release.** Em desenvolvimento rumo à v0.1.0. As interfaces podem mudar.

Inventário de clusters Kubernetes em camadas de tempo, para planejar migração. O `kubestrata` lê
os contextos do kubeconfig, coleta um inventário somente leitura e sanitizado num cache local e
gera uma visão HTML offline: **recursos mais novos no centro, mais antigos na borda**, agrupados
por namespace e time. A visão de comparação põe dois clusters lado a lado, e cada onda de
migração aparece como um anel no destino.

- Binário Go único, sem servidor, sem conta, sem telemetria.
- Somente leitura: só `list` e `get`. Valores de Secret nunca saem do cluster.
- HTML offline: abra o `index.html` gerado direto do disco.

## Início rápido

Instale com `go install` (Go 1.27 ou mais novo). O binário fica em `$(go env GOPATH)/bin`, que
precisa estar no seu `PATH`:

```sh
go install github.com/leosavio/kubestrata/cmd/kubestrata@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

Ou compile a partir de um clone (o `make build` grava `bin/kubestrata`; o `export` põe o binário
no `PATH` deste shell):

```sh
git clone https://github.com/leosavio/kubestrata.git && cd kubestrata
make build && export PATH="$PWD/bin:$PATH"
```

Depois rode com um contexto de origem e um de destino do seu kubeconfig (o `kubestrata contexts`
lista os contextos):

```sh
kubestrata collect -c <origem> -c <destino>         # inventário somente leitura no cache local
kubestrata render -c <origem> --open                # visão radial de um cluster
kubestrata compare -c <origem> -c <destino> --open  # lado a lado, com a cobertura por namespace
```

`render` e `compare` só leem o cache. Rode `collect --force` para atualizá-lo.

### Sem cluster à mão? Teste no lab do kuberito

O [kuberito](https://github.com/leosavio/kuberito) (ainda não público) sobe dois clusters locais pequenos no Docker,
uma origem parecida com OpenShift e um destino parecido com EKS, com uma migração em andamento.
Instale como o README dele descreve e depois:

```sh
kuberito up && eval "$(kuberito env)"     # KUBECONFIG=~/.kuberito/kubeconfig neste shell
kubestrata collect -c kuberito-source -c kuberito-target
kubestrata compare -c kuberito-source -c kuberito-target --open
```

## Telas

Visão radial do `kuberito-source`:

![Visão radial do cluster kuberito-source do lab: um setor por namespace com o seu time, recursos coloridos por categoria, os mais novos no centro e os mais antigos na borda em anéis de 30 dias, e um DeploymentConfig obsoleto do legacy-portal selecionado com os fatos e os objetos relacionados no painel lateral](docs/images/radial-kuberito-source.png)

Visão de comparação entre `kuberito-source` e `kuberito-target`:

![Visão de comparação de kuberito-source e kuberito-target lado a lado: recursos coloridos por estado (equal verde, drift âmbar, pending vermelho, new azul), o Deployment payments/api com drift selecionado nos dois painéis com a tag de imagem 2.56 contra 2.57 no painel lateral, e a tabela de cobertura por namespace abaixo dos painéis](docs/images/compare-kuberito-source-target.png)

## Comandos

| Comando | Faz |
|---|---|
| `kubestrata contexts` | contextos do kubeconfig, sem acessar cluster |
| `kubestrata collect -c source -c target` | snapshot somente leitura no cache local; `--snapshot-out <arquivo\|dir>` também grava o snapshot em JSON puro |
| `kubestrata cache ls` | snapshots no cache, com o `STAMP` que o `diff` recebe |
| `kubestrata render -c source --open` | visão radial, HTML offline |
| `kubestrata compare -c source -c target` | equal / drift / pending / new e cobertura, HTML lado a lado |
| `kubestrata diff -c source` | dois snapshots do mesmo cluster (o mais novo e o anterior, ou `--from`/`--to`): new / gone / drift / equal |
| `kubestrata` | TUI interativa (planejada) |

`--format json` imprime um documento para máquina com `schema_version` 1. `--redact` mascara IDs
de conta AWS e hosts de endpoint EKS/AKS na saída e no site; o cache guarda os valores reais.

Escalas de anéis (`render`/`compare --scale`, também trocáveis na página): `30m` e `1h` (12 anéis
de 30 minutos ou 1 hora, para cluster novo), `5d`, `10d`, `30d`, `log` e anéis custom com sufixo de
unidade, `--rings 30m,2h,1d,7d,30d` (número sem sufixo é dia).

O `compare` pareia recursos por `group/Kind/namespace/name` (os nomes dos estados ficam em inglês
na saída):

| Estado | Significado |
|---|---|
| equal | nos dois clusters, mesmo fingerprint |
| drift | nos dois, o fingerprint difere; a página e o JSON listam os fatos que diferem |
| pending | só na origem: ainda não migrado |
| new | só no destino |

Cobertura por namespace = (equal + drift) / recursos na origem. Os namespaces de sistema ficam fora
dos totais.

## Design

Ver [docs/design.pt-BR.md](docs/design.pt-BR.md) (versão oficial em inglês:
[docs/design.md](docs/design.md)).

## Como contribuir

Veja [CONTRIBUTING.pt-BR.md](CONTRIBUTING.pt-BR.md), [SECURITY.md](SECURITY.md) e [CHANGELOG.md](CHANGELOG.md).

## Licença

[MIT](LICENSE)
