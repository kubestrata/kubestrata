# Análise de segurança

[English](security-analysis.md)

Esta página lista as checagens de segurança que rodam no kubestrata, como repeti-las numa release
baixada e todos os achados até agora, com o que foi feito com cada um. O que o kubestrata lê, guarda
e nunca imprime, e como reportar uma vulnerabilidade, estão no [SECURITY.md](../SECURITY.md).

Os resultados abaixo têm data. As checagens continuam rodando no CI, e a aba **Security** do
repositório mostra os alertas abertos agora.

## Checagens

| Checagem | Encontra | Roda | Bloqueia |
|---|---|---|---|
| [`govulncheck`](https://go.dev/doc/security/vuln/) v1.10.0 ([`security.yml`](../.github/workflows/security.yml)) | vulnerabilidades conhecidas da base do Go, na biblioteca padrão e nos módulos, que o código do kubestrata alcança (grafo de chamadas) | todo pull request, todo push na `main`, toda semana | sim, com qualquer vulnerabilidade alcançável |
| [`zizmor`](https://docs.zizmor.sh/) 1.33.1 ([`security.yml`](../.github/workflows/security.yml)) | pontos fracos de GitHub Actions: permissões, gatilhos perigosos, injeção de template, arquivos de ambiente, actions sem pin | todo pull request, todo push na `main`, toda semana | sim, com qualquer achado |
| CodeQL (configurações do repo, default setup) | análise estática do Go, do JavaScript da página e dos workflows | todo pull request, todo push na `main`, toda semana | alerta no pull request e em **Security** |
| Alertas e security updates do Dependabot | advisories dos módulos do `go.mod` e das actions | continuamente | abre pull request com a correção |
| Atualizações de versão do Dependabot ([`dependabot.yml`](../.github/dependabot.yml)) | módulos mais novos (Kubernetes e `golang.org/x` em grupo) e actions | toda semana | abre pull request |
| Secret scanning e push protection | credenciais commitadas no repositório | a cada push | push com segredo detectado é recusado |
| Cobertura dos testes unitários ([`ci.yml`](../.github/workflows/ci.yml)) | a fatia de statements que os testes unitários executam, impressa no resumo do job | todo pull request, todo push na `main` | não: medida, sem limite mínimo ainda |
| [OpenSSF Scorecard](https://scorecard.dev) ([`scorecard.yml`](../.github/workflows/scorecard.yml)) | práticas de cadeia de suprimentos do repositório: dependências fixadas, permissões de token, proteção de branch, releases assinadas, SAST, ... | todo push na `main`, semanal | não: resultados no code scanning e no badge do README |
| `go vet`, `staticcheck` ([`ci.yml`](../.github/workflows/ci.yml)) | padrões de bug | todo pull request, todo push na `main` | sim |
| [`install-smoke.yml`](../.github/workflows/install-smoke.yml) | o binário instalado nunca imprime a credencial do kubeconfig (`contexts` num kubeconfig de teste com token) | depois de cada release, toda semana | sim |
| [`lab.yml`](../.github/workflows/lab.yml) | num lab do kuberito (release fixada, checksum e assinatura cosign conferidos): nenhum valor de segredo semeado, `last-applied-configuration` ou ID de conta em snapshot algum (`TestLabNoSecretLeak`), e as páginas carregam sob a Content-Security-Policy sem erro (`make page-check`) | todo pull request, todo push na `main`, toda semana, depois que a variável `KUBERITO_PUBLIC` for `true` | sim |

Os achados da biblioteca padrão dependem da versão do Go que compila o binário, que é a linha `go`
do [`go.mod`](../go.mod) (o workflow de release lê essa linha). Um advisory novo do Go reprova o
`govulncheck` semanal mesmo sem mudança aqui; a correção é subir o toolchain e publicar release.

## Cadeia de suprimentos

- Toda action está fixada pelo SHA do commit. Os workflows têm `contents: read` por padrão; só o
  job de release (`contents: write`, `id-token: write`, `attestations: write`), o job do PyPI
  (`id-token: write`) e o job que dispara o smoke de instalação (`actions: write`) recebem mais.
- Os archives da release saem com `CGO_ENABLED=0`, `-trimpath` e metadados fixos no tar. O
  `checksums.txt` é assinado com cosign keyless (Sigstore, OIDC do GitHub); o README mostra como
  verificar um archive.
- As releases depois da 0.1.4 trazem attestations de artefato do GitHub
  (`actions/attest-build-provenance`, `actions/attest-sbom`): build provenance SLSA para todo
  archive do `checksums.txt` e um SBOM CycloneDX por archive (`cyclonedx-gomod` v1.12.0: os módulos
  e a biblioteca padrão daquele build GOOS/GOARCH), anexado à release e com attestation. O
  `gh attestation verify <archive> -R kubestrata/kubestrata` confere a provenance sem cosign;
  acrescente `--predicate-type https://cyclonedx.org/bom` para o SBOM.
- Os wheels do PyPI são publicados por Trusted Publishing com attestations PEP 740:
  `uvx pypi-attestations verify pypi --repository https://github.com/kubestrata/kubestrata
  pypi:<nome do wheel>` confere um deles.
- A fórmula do Homebrew fixa cada archive pelo `sha256`. O workflow de release atualiza o
  [kubestrata/homebrew-tap](https://github.com/kubestrata/homebrew-tap) com uma deploy key que só
  escreve nesse repositório.

## Repetir as checagens numa release

```sh
v=0.1.3
gh release download "v${v}" -R kubestrata/kubestrata -p "kubestrata_${v}_linux_amd64.tar.gz"
tar -xzf "kubestrata_${v}_linux_amd64.tar.gz" kubestrata
go version kubestrata                                   # toolchain do Go que compilou o binário
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 -mode binary ./kubestrata
```

Num clone, `go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 -show verbose ./...` lista também
vulnerabilidades em módulos que o código não chama.

## Achados

### 2026-11-26, versão 0.1.2 (commit `9f1f2b6`)

| Ferramenta | Achado | Desfecho |
|---|---|---|
| `govulncheck` | 11 vulnerabilidades da biblioteca padrão alcançáveis com Go 1.27.1, em `net/http`, no cliente HTTP/2, `crypto/tls`, `crypto/x509` e `net/textproto`: GO-2026-6859, GO-2026-6809, GO-2026-6617, GO-2026-6613, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6608, GO-2026-6607, GO-2026-6605, GO-2026-6603. Corrigidas no Go 1.27.2 a 1.27.4 | corrigido na 0.1.3: compilada com Go 1.27.4 |
| `govulncheck` | mais 7 em módulos, não alcançadas pelo código (entre elas `golang.org/x/net` v0.57.0) | corrigido na 0.1.3: `golang.org/x/net` v0.62.0 |
| `zizmor` | `dangerous-triggers` (`workflow_run` no `install-smoke.yml`) e 3 × `github-env` (escrita em `GITHUB_ENV`) | corrigido na 0.1.3: o workflow de release dispara o smoke e os valores passam por step outputs |
| [`gosec`](https://github.com/securego/gosec) | 6 achados, nenhum crítico | aceitos, ver abaixo |

As versões 0.1.0 a 0.1.2 têm as vulnerabilidades da biblioteca padrão acima. Atualize para a 0.1.3
ou mais nova.

### 2026-11-27, versão 0.1.3 (commit `6b4a976`)

| Ferramenta | Resultado |
|---|---|
| `govulncheck` no código, `-show verbose` | nenhuma vulnerabilidade |
| `govulncheck -mode binary` no archive linux/amd64 publicado | nenhuma vulnerabilidade; `go version` mostra go1.27.4 |
| CodeQL (Go, Actions, JavaScript/TypeScript) | 0 alertas |
| Alertas do Dependabot, secret scanning | 0 abertos |
| `zizmor` | nenhum achado |

### Achados do `gosec` aceitos

O `gosec` rodou à mão na 0.1.2; ele não faz parte do CI. Cada achado, com o motivo de ficar:

| Regra | Onde | Por que fica |
|---|---|---|
| G204, subprocesso com variável | `internal/cli/render.go`, `openBrowser` | só roda com `--open`. O programa é fixo (`open` no macOS, `xdg-open` nos demais) e o argumento é o caminho da página que o kubestrata acabou de gravar. Não passa por shell. |
| G302, permissão acima de `0600` | `internal/cache/cache.go`, `os.Chmod(dir, 0o700)` | diretório precisa do bit de execução para ser aberto; `0700` é só do dono. Os arquivos do cache são `0600`. |
| G104, erro não checado (4 ×) | `internal/cache/cache.go` `writeAtomic` e o gravador de snapshot em `internal/cli/collect.go`: `tmp.Close()` num caminho de erro | a função já devolve o primeiro erro, e um `os.Remove` em `defer` apaga o arquivo temporário, então um segundo erro do `Close` não acrescenta nada. |
