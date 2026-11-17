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
kubestrata contexts                       # contextos do kubeconfig (já disponível)
kubestrata collect -c origem -c destino   # snapshot somente leitura no cache local
kubestrata render -c origem --open        # visão radial de um cluster
kubestrata compare -c origem -c destino   # visão lado a lado
kubestrata                                # TUI interativa
```

## Design

Ver [docs/design.md](docs/design.md) (em inglês).

## Licença

[MIT](LICENSE)
