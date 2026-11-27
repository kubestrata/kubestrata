# kubestrata

[![CI](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/kubestrata/kubestrata/actions/workflows/ci.yml?query=branch%3Amain)
[![security](https://github.com/kubestrata/kubestrata/actions/workflows/security.yml/badge.svg?branch=main)](docs/security-analysis.pt-BR.md)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8)](go.mod)

[English](README.md)

> **Status: 0.1.x, primeiras releases.** As interfaces ainda podem mudar antes da 1.0
> ([CHANGELOG](CHANGELOG.md)).

Inventário de clusters Kubernetes em camadas de tempo, para planejar migração. O `kubestrata` lê
os contextos do kubeconfig, coleta um inventário somente leitura e sanitizado num cache local e
gera uma visão HTML offline: **recursos mais novos no centro, mais antigos na borda**, agrupados
por namespace e time. A visão de comparação põe dois clusters lado a lado, e cada onda de
migração aparece como um anel no destino.

- Binário Go único, sem servidor, sem conta, sem telemetria.
- Somente leitura: só `list` e `get`. Valores de Secret nunca saem do cluster.
- HTML offline: abra o `index.html` gerado direto do disco.

## Instalação

Homebrew (macOS e Linux), pelo tap [`kubestrata/homebrew-tap`](https://github.com/kubestrata/homebrew-tap):

```sh
brew install kubestrata/tap/kubestrata
```

O Homebrew 7.0.11 em diante recusa fórmula de tap não oficial até ela ser confiada. Se o
`brew install` parar com "Refusing to load formula ... from untrusted tap", rode
`brew trust --formula kubestrata/tap/kubestrata` e instale de novo.

PyPI (macOS 12+ e Linux, arm64/amd64), com [uv](https://docs.astral.sh/uv/) ou pipx. O wheel traz
o mesmo binário Go assinado do archive da release; não há código Python:

```sh
uvx kubestrata --version        # roda uma vez, sem instalar
uv tool install kubestrata      # ou: pipx install kubestrata
```

Com Go instalado (Go 1.27 ou mais novo). O binário fica em `$(go env GOPATH)/bin`, que precisa
estar no seu `PATH`:

```sh
go install github.com/kubestrata/kubestrata/cmd/kubestrata@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

Release (darwin/linux, arm64/amd64), com verificação de checksum e assinatura keyless:

```sh
v=0.1.4; os=darwin; arch=arm64
base="https://github.com/kubestrata/kubestrata/releases/download/v${v}"
curl -fsSLO "${base}/kubestrata_${v}_${os}_${arch}.tar.gz"
curl -fsSLO "${base}/checksums.txt"
curl -fsSLO "${base}/checksums.txt.sigstore.json"
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/kubestrata/kubestrata/.github/workflows/release.yml@refs/tags/v${v}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
grep " kubestrata_${v}_${os}_${arch}.tar.gz\$" checksums.txt | shasum -a 256 -c -
tar -xzf "kubestrata_${v}_${os}_${arch}.tar.gz" kubestrata
```

No Linux, `sha256sum -c -` faz o mesmo que `shasum -a 256 -c -`. Para compilar a partir de um
clone, o `make build` grava `bin/kubestrata`.

### Plataformas testadas

| Plataforma | Runner do GitHub | Smoke de instalação (`install-smoke.yml`) |
|---|---|---|
| linux/amd64 | `ubuntu-24.04` | brew · go install · tarball · uv |
| linux/arm64 | `ubuntu-24.04-arm` | brew · go install · tarball · uv |
| darwin/arm64 | `macos-15` | brew · go install · tarball · uv |
| darwin/amd64 | `macos-15-intel` | brew · go install · tarball · uv |

O [`install-smoke.yml`](.github/workflows/install-smoke.yml) instala uma release publicada como o
usuário faria, depois de cada release (disparado pelo `release.yml`), toda terça e sob demanda
(`gh workflow run install-smoke.yml -f version=vX.Y.Z`). Em cada caminho de instalação ele confere
que o `kubestrata --version` bate com a tag, que o `contexts` lê um kubeconfig de teste sem
imprimir a credencial e que o `cache ls` funciona com o cache vazio
([`scripts/smoke-check.sh`](scripts/smoke-check.sh)).

## Início rápido

Rode com um contexto de origem e um de destino do seu kubeconfig (o `kubestrata contexts` lista
os contextos):

```sh
kubestrata collect -c <origem> -c <destino>         # inventário somente leitura no cache local
kubestrata render -c <origem> --open                # visão radial de um cluster
kubestrata compare -c <origem> -c <destino> --open  # lado a lado, com a cobertura por namespace
```

`render` e `compare` só leem o cache. Rode `collect --force` para atualizá-lo.

### Sem cluster à mão? Teste no lab do kuberito

O [kuberito](https://github.com/kuberito/kuberito) (ainda não público) sobe dois clusters locais pequenos no Docker,
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

## Segurança

- Somente leitura por construção: as credenciais do kubeconfig são usadas, nunca guardadas nem
  impressas, e os Secrets são lidos só como metadados ([SECURITY.md](SECURITY.md)).
- A cada mudança e toda semana: `govulncheck`, `zizmor`, CodeQL, Dependabot e secret scanning.
- Última análise, 2026-11-27 na 0.1.3: nenhuma vulnerabilidade conhecida alcançável pelo código ou
  presente no binário publicado, e nenhum alerta aberto. As versões 0.1.0 a 0.1.2 foram compiladas
  com Go 1.27.1 e têm 11 vulnerabilidades da biblioteca padrão: atualize para a 0.1.3 ou mais nova.

Checagens, histórico de achados e como repeti-las numa release:
[docs/security-analysis.pt-BR.md](docs/security-analysis.pt-BR.md) (versão oficial em inglês:
[docs/security-analysis.md](docs/security-analysis.md)). Para reportar uma vulnerabilidade, veja o
[SECURITY.md](SECURITY.md).

## Design

Ver [docs/design.pt-BR.md](docs/design.pt-BR.md) (versão oficial em inglês:
[docs/design.md](docs/design.md)).

## Como contribuir

Veja [CONTRIBUTING.pt-BR.md](CONTRIBUTING.pt-BR.md), [SECURITY.md](SECURITY.md) e [CHANGELOG.md](CHANGELOG.md).

## Licença

[MIT](LICENSE)
