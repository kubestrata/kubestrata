# Como contribuir

[English](CONTRIBUTING.md)

Issues e pull requests são bem-vindos. Regras curtas:

| Tema | Regra |
|---|---|
| Escopo | inventário somente leitura, visão offline, comparação. Um verbo novo no cluster (qualquer coisa além de `list`/`get`) começa como issue |
| Sanitização | kind ou fato novo não pode levar valor de Secret, env, args, command ou annotation; estenda o teste de vazamento em `internal/collect` |
| Determinismo | mesmo snapshot, mesmos bytes na saída: sem relógio, sem iteração de map sem ordenar |
| Contrato do snapshot | campos só crescem; mudança de significado sobe o `schema_version` e o leitor recusa versão maior |
| Visão web | sem rede (CSP), texto não confiável só por `textContent`; rode `make page-check` |
| Verificação | `make check` (gofmt, vet, staticcheck, `go test -race`, build); com um lab kuberito, `make lab-test` |
| Workflows | actions fixadas pelo SHA completo com a tag em comentário; `permissions:` mínimas; `persist-credentials: false` |
| Commits | em inglês, no imperativo (`feat:`, `fix:`, `docs:`, `chore:`) |
| Docs | os arquivos em inglês são a versão oficial; mantenha a tradução `*.pt-BR.md` em dia no mesmo PR quando possível |

Design: [docs/design.pt-BR.md](docs/design.pt-BR.md).
