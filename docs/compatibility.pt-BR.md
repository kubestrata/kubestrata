# Compatibilidade

[English](compatibility.md)

O kubestrata está antes da 1.0. Esta página diz em quais saídas dá para construir automação hoje,
o que ainda pode mudar e a política proposta a partir da 1.0.

## O que o `schema_version` cobre

Todo documento JSON que o kubestrata imprime ou grava traz `schema_version`, e cada um tem um JSON
Schema (draft 2020-12) em [`schemas/`](../schemas/):

| Documento | Gerado por | Schema |
|---|---|---|
| snapshot | os arquivos do cache, o `collect --snapshot-out`, o `json/<cluster_id>.json` do site | [`snapshot.schema.json`](../schemas/snapshot.schema.json) |
| contexts | `contexts --format json` | [`contexts.schema.json`](../schemas/contexts.schema.json) |
| collect | `collect --format json` | [`collect.schema.json`](../schemas/collect.schema.json) |
| cache ls | `cache ls --format json` | [`cache-ls.schema.json`](../schemas/cache-ls.schema.json) |
| render | `render --format json` | [`render.schema.json`](../schemas/render.schema.json) |
| compare | `compare --format json`, o `json/compare.json` do site | [`compare.schema.json`](../schemas/compare.schema.json) |
| diff | `diff --format json` | [`diff.schema.json`](../schemas/diff.schema.json) |
| summary | `summary --format json` | [`summary.schema.json`](../schemas/summary.schema.json) |

Os testes conferem as saídas reais contra esses schemas, de forma estrita: um campo que o código
grava e o schema não lista quebra o build (`go test ./schemas`, e o e2e do lab para os snapshots e
a comparação do lab do kuberito).

Dentro do `schema_version` 1:

- Campos só crescem. Um campo novo é opcional; um campo existente mantém nome, tipo e significado.
- Os conjuntos fechados de valores, marcados com `enum` nos schemas, não mudam: os estados do
  `compare` (`equal`, `drift`, `pending`, `new`) e do `diff` (`equal`, `drift`, `gone`, `new`), o
  status do `collect`, os motivos de unowned do `summary`, o `mode: "diff"`.
- Os conjuntos abertos de valores podem ganhar valores: `cat`, `manager`, o `rel` das arestas, os
  nomes de fato em `facts` e os valores deles, `distro`, o `auth` de um contexto. Trate um valor
  desconhecido como "outro".
- Um consumidor ignora campos que não conhece e recusa um `schema_version` maior que o suportado.
- Uma mudança de significado leva o `schema_version` para 2, e o próprio kubestrata recusa um
  snapshot mais novo do que sabe ler.

## O que pode mudar antes da 1.0

- Flags, os padrões delas (a escala padrão, `--max-age`) e nomes de comando; uma flag pode ser
  renomeada ou removida, com nota no [CHANGELOG](../CHANGELOG.md).
- A saída em texto (tabelas, o Markdown do `summary`, notas no stderr): é para pessoas, não para
  parse. Use `--format json`.
- A projeção de cada kind: quais fatos existem, os nomes deles dentro de `facts` e, portanto, os
  fingerprints. Entre snapshots de duas versões, o `compare` e o `diff` só contam os fatos que os
  dois lados têm e listam o resto em `collector_change` / `collector_facts`.
- A página: a config da visão embutida no `index.html`, os parâmetros do hash da URL, o layout e os
  atributos `data-*` do `<body>`.
- O layout do cache no diretório de cache (`clusters/<id>/<stamp>.json.gz`, `contexts.json`).
- O próprio `schema_version`, se um documento precisar mudar de significado antes da 1.0.

## Política proposta a partir da 1.0

- Versionamento semântico do binário. Dentro de uma versão major, os documentos JSON só mudam por
  acréscimo (campos opcionais novos, valores novos nos conjuntos abertos); o `schema_version` só
  muda junto com uma versão major.
- Uma flag a remover ou renomear é depreciada antes: listada no CHANGELOG, ainda aceita com um
  aviso no stderr por pelo menos uma versão minor, removida só numa versão major.
- Os códigos de saída ficam: 0 em sucesso, diferente de 0 quando um comando falha ou a coleta falha
  para um contexto.
- A saída em texto e a página podem mudar em qualquer release.
- Os schemas são publicados com cada release, na mesma tag.
