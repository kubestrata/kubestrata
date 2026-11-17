# kubestrata

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

## Comandos (meta da v0.1.0)

```sh
kubestrata contexts                       # contextos do kubeconfig (disponível)
kubestrata collect -c source -c target    # snapshot somente leitura no cache local (disponível)
kubestrata cache ls                       # snapshots no cache (disponível)
kubestrata render -c source --open        # visão radial de um cluster
kubestrata compare -c source -c target    # visão lado a lado
kubestrata                                # TUI interativa
```

## Design

Ver [docs/design.pt-BR.md](docs/design.pt-BR.md) (versão oficial em inglês:
[docs/design.md](docs/design.md)).

## Licença

[MIT](LICENSE)
