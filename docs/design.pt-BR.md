# Design do kubestrata

[English](design.md)

Status: design pré-release da v0.1.0 (F1). As decisões aqui valem até o código dizer o contrário.
A versão em inglês (`design.md`) é a oficial; esta é a tradução.

## Objetivo

Inventário somente leitura dos clusters do kubeconfig, guardado num cache local e exibido numa
visão HTML offline em que **os recursos recentes ficam no centro e os antigos na borda**, mais uma
**comparação lado a lado de dois clusters**. O público é quem planeja uma migração de cluster: o
que existe, quem é o dono, como chegou lá, o que tem estado, o que está exposto e o que ainda está
mudando.

## Fluxo

```mermaid
flowchart LR
  K[contextos do kubeconfig] --> C[collect: client-go, só list/get]
  C --> P[projeção + sanitização por kind]
  P --> S[(cache: ~/Library/Caches/kubestrata)]
  S --> R[render: offline, determinístico]
  R --> O[site: index.html + data/*.js + json/]
  O --> B[navegador, file://]
```

`render` e `compare` nunca falam com cluster. Atualizar o cache é sempre explícito
(`collect --force`, `--max-age` ou `r` na TUI).

## Eixo do tempo

| Campo | Significado | Uso |
|---|---|---|
| `metadata.creationTimestamp` | criação do objeto; zera quando o CI apaga e reaplica | eixo padrão |
| maior `managedFields[].time`, **ignorando `subresource: status`** | última mudança de spec/metadata por qualquer manager | eixo `alterado`: o que ainda está em movimento |
| idade do Pod | último rollout ou reagendamento, não a idade do workload | não vira eixo: pods são agregados no dono |

Os updates de status são ignorados de propósito: controllers gravam status o tempo todo e tudo
pareceria recente.

## Layout: polar determinístico, não força-dirigido

Ângulo = setor do namespace (agrupado por time, tamanho proporcional ao número de objetos), com
sub-setor por categoria. Raio = idade na escala de anéis escolhida. As arestas (Ingress → Service →
Workload → PVC / Secret / ConfigMap / ServiceAccount) aparecem só no hover ou na seleção.

| Critério | Força-dirigido | Polar (escolhido) |
|---|---|---|
| Escala | repulsão O(n²), serve para centenas | O(n); colisões resolvidas numa grade espacial |
| Comparação | o mesmo objeto cai em lugares diferentes | setores calculados sobre A∪B: mesmo namespace, mesmo ângulo nos dois lados |
| Determinismo | depende da simulação | mesma entrada, mesma imagem |

As escalas de anéis trocam no navegador sem regenerar: `5d`, `10d`, `30d` (12 anéis lineares mais
uma faixa externa comprimida), `log` (7d / 30d / 90d / 1a+, saturando em 3 anos) e custom
`--rings 5,15,30,90,180,365`.

## Coleta

- Kubeconfig: regras padrão do client-go (`KUBECONFIG` com vários arquivos, `--kubeconfig`); exec
  plugins funcionam sem mudança. Falha de auth num contexto vira erro estruturado e não para os
  outros.
- Identidade do cluster: `sha256(URL do servidor + UID do namespace kube-system)[:12]`. Nome de
  contexto não é identidade.
- Discovery (`ServerPreferredResources`): kinds servidos, CRDs e tipos do OpenShift sem lista fixa
  no código. A distro sai dos grupos de API, da URL, da versão e dos labels de node, e os sinais
  usados ficam em `distro_signals` (um OpenShift simulado com CRDs no kind aparece como tal).
- Carga no API server: só `list`/`get`, paginação `limit=500`, QPS/burst explícitos, no máximo 4
  contextos e 8 kinds em paralelo por contexto.
- RBAC: um `403` num kind vai para `errors[]` e a coleta continua. Listar Secrets dá acesso de
  leitura aos valores, mesmo que o kubestrata só peça metadata; a documentação traz um ClusterRole
  `inventory-reader` sugerido.

## Sanitização (fronteira de segurança)

O snapshot guarda uma projeção curada, nunca o objeto bruto.

| Risco | Tratamento |
|---|---|
| Valores de Secret | Secrets são lidos pela Table do servidor (`as=Table;g=meta.k8s.io;v=v1`, `includeObject=Metadata`): tipo e número de chaves, sem `data`. Linha que não vier como `PartialObjectMetadata` é recusada |
| `kubectl.kubernetes.io/last-applied-configuration` | descartada de todo objeto: num Secret aplicado com `kubectl apply` ela guarda o `data` em base64 |
| `env[].value` literal, `args`, `command` | só os nomes; valores descartados |
| ConfigMap | nomes de chave + hash do conteúdo (para drift), sem valores |
| `managedFields` | só `manager`, `operation`, `time`, `subresource` |
| `spec.tls.key` / `certificate` de Route do OpenShift | descartados: fica só o tipo de terminação |
| Annotations de identidade de nuvem (ARN do IRSA, ids de workload identity) | só o tipo (`aws-irsa`, ...), nunca o ARN ou o id |
| Annotations em geral | não são guardadas; os fatos saem das poucas que importam |
| Cache e site | `0700` / `0600` no diretório de cache do usuário, nunca em git |
| XSS na página local | nomes e labels são dado não confiável: `json.Marshal` (escapa `<>&`) grava o `data/*.js` e a página renderiza com `textContent` |

Um teste semeia valores conhecidos em Secrets, env, args, command, annotations, Route e CR, e falha
se qualquer um deles aparecer em qualquer byte do snapshot.

## Kinds e categorias padrão

| Categoria | Kinds | Observação |
|---|---|---|
| workload | Deployment, StatefulSet, DaemonSet, CronJob, Job sem dono, DeploymentConfig, Pod e ReplicaSet **órfãos** | pods agregados no dono; pod órfão é alerta de migração |
| network | Service (sem o `default/kubernetes`), Ingress, Route, Gateway, HTTPRoute, NetworkPolicy | hosts e TLS no painel |
| config | ConfigMap (sem `kube-root-ca.crt`, `openshift-service-ca.crt`), Secret, ServiceAccount (sem o `default`, salvo com identidade de nuvem ou pull secret) | Secrets `helm.sh/release.v1` agrupados como estado do Helm |
| storage | PVC, PV, StorageClass, VolumeSnapshot | kinds de escopo de cluster ganham setor próprio |
| policy | HPA, PDB, ResourceQuota, LimitRange, Role, RoleBinding | PDB afeta drain e cutover |
| CRDs | recursos customizados | agrupados por API group |
| sistema | namespaces que casam com `^(kube-|openshift|amazon-|aws-|gke-|azure-|local-path-storage$)` | escondidos por padrão (`--system-ns`) |

## Contrato do snapshot (`schema_version: 1`)

```json
{
  "schema_version": 1,
  "tool": {"name": "kubestrata", "version": "0.1.0"},
  "collected_at": "2026-11-17T12:12:00Z",
  "cluster": {"id": "3f9a1c0b7e21", "context": "source", "server_host": "api.example:6443",
              "distro": "openshift", "distro_signals": ["group route.openshift.io"], "version": "v1.27.x",
              "nodes": {"count": 12, "arch": {"amd64": 12}, "zones": ["a", "b"]}},
  "namespaces": [{"name": "payments", "team": "team-payments", "created": "2024-06-01T00:00:00Z"}],
  "resources": [{
    "id": "apps/Deployment/payments/api", "cat": "workload", "ns": "payments", "name": "api",
    "created": "2025-04-24T10:00:00Z", "changed": "2026-10-02T18:30:00Z",
    "manager": "helm", "app": "payments-api", "team": "team-payments",
    "facts": {"replicas": 3, "ready": 3, "pods": 3, "restarts": 1, "images": ["registry/pay/api:2.14"]},
    "fp": "sha256:..."
  }],
  "edges": [{"from": "networking.k8s.io/Ingress/payments/api", "to": "core/Service/payments/api", "rel": "routes"}],
  "errors": [{"kind": "core/Secret", "ns": "data", "reason": "Forbidden"}]
}
```

| `rel` | De → para |
|---|---|
| `routes` | Ingress, Route, HTTPRoute → Service |
| `selects` | Service, PodDisruptionBudget → workload (selector ⊆ labels do pod template) |
| `mounts` | workload → PVC, Secret, ConfigMap (volumes); VolumeSnapshot → PVC |
| `env-from` | workload → Secret, ConfigMap (`envFrom`, `valueFrom`) |
| `uses-sa` | workload → ServiceAccount |
| `pull-secret` | workload → Secret (`imagePullSecrets`) |
| `owns` | dono controlador → objeto exibido (ex.: recurso customizado → seu Deployment) |
| `binds` | PVC → PersistentVolume, StorageClass |
| `tls` | Ingress, Gateway → Secret |
| `scales` | HPA → alvo |
| `grants` | RoleBinding → Role, ServiceAccount |
| `attaches` | HTTPRoute → Gateway |

As arestas são calculadas em Go, não no navegador. Referência a objeto que não existe vai para
`facts.missing_refs`; Service cujo selector não casa com nenhum workload ganha
`facts.selects_none`. Consumidor que recebe `schema_version` maior que o suportado recusa o
documento.

Pods, ReplicaSets, ReplicationControllers e Jobs são agregados no dono exibido mais próximo
(`pods`, `ready`, `restarts`, `pod_newest`, `pod_oldest`, `archs`, `last_rollout`, `jobs`,
`last_run`). Sem dono, aparecem com `facts.orphan`.

## Comparação

`kubestrata compare -c <origem> -c <destino>` lê o snapshot mais novo de cada contexto no cache e
nunca fala com cluster. A comparação é calculada em Go (`internal/compare`), então a tabela da CLI,
o documento JSON e a página mostram os mesmos números. O JSON guarda a cobertura com 4 casas; a CLI
(`math.Round`) e a página (`Math.round`) arredondam a porcentagem com a metade para cima, então
0,125 aparece como 13% nas duas.

Chave de identidade: o id do recurso `group/Kind/namespace/name`. Estados (os nomes ficam em
inglês na CLI, no JSON e na página):

| Estado | Significado | Cor |
|---|---|---|
| equal (igual) | nos dois, mesmo fingerprint (`fp`) | `#66bb6a` |
| drift | nos dois, fingerprint diferente; o detalhe lista os fatos que diferem | `#ffb300` |
| pending (pendente) | só na origem: ainda não migrado | `#ef5350` |
| new (novo) | só no destino | `#42a5f5` |

O fingerprint é um hash do kind e dos fatos projetados, **menos um conjunto volátil** (contagem de
pods, prontidão, restarts, horários de pod e de rollout, execuções de Job, arquiteturas, field
managers, referências ausentes, revisão e status do Helm, marcas de órfão). Um hash do spec inteiro
acusaria diferenças de default entre versões do Kubernetes; a projeção por kind decide o que conta:

| Kind | Fatos no fingerprint (principais) |
|---|---|
| Deployment, StatefulSet, DaemonSet, DeploymentConfig | imagens, réplicas, requests/limits de CPU e memória, service account, nomes de PVC, node selector (o DeploymentConfig também strategy e triggers) |
| Service | type, ports |
| Ingress, Route, HTTPRoute | hosts, paths, nomes dos Secrets de TLS, class |
| PVC | storageClass, tamanho, accessModes |
| ConfigMap | nomes de chave + hash do conteúdo |
| Secret | tipo + número de chaves |
| ServiceAccount | tipo de identidade de nuvem (`aws-irsa`, ...), número de image pull secrets |

Referências de volume, envFrom e Secret/ConfigMap são arestas, não fatos: não entram no
fingerprint.

Detalhe do drift = todo fato fora do conjunto volátil cujo valor difere, com os dois valores (o lado
que não tem o fato fica sem valor). Como a chave inclui group/Kind, uma **tradução de kind** aparece
como par: uma Route do OpenShift ou um Ingress trocado por uma HTTPRoute da Gateway API fica
pendente na origem e novo no destino. Parear kinds diferentes é das unidades de migração da F2.
Objetos com nome gerado (PersistentVolumes `pvc-<uid>`) também nunca formam par.

Resumo por namespace (`_` = escopo de cluster): total na origem, total no destino, equal, drift,
pending, new e cobertura = (equal + drift) / total na origem, `null` quando a origem não tem nada
ali. Os totais e a tabela padrão deixam de fora os namespaces de sistema, como a visão padrão.

`--format json` imprime um documento (`schema_version` 1) com `source`, `target`, `totals`,
`namespaces`, `items` (`id`, `ns`, `kind`, `state`, `diff[]`) e o `index` e a `url` do site:

```json
{"schema_version": 1,
 "source": {"cluster_id": "d1f6c4078d11", "context": "source", "distro": "openshift", "resources": 406},
 "totals": {"ns": "total", "source": 320, "target": 147, "equal": 90, "drift": 44, "pending": 186, "new": 13, "coverage": 0.4188},
 "items": [{"id": "apps/Deployment/payments/api", "ns": "payments", "kind": "Deployment", "state": "drift",
            "diff": [{"fact": "images", "source": ["registry/api:2.56"], "target": ["registry/api:2.57"]}]}]}
```

A página põe os dois clusters lado a lado com setores calculados sobre os dois (mesmo namespace,
mesmo ângulo), zoom e pan sincronizados e o modo de cor `state` por padrão (categoria, manager e
time continuam disponíveis). O hover ou a seleção de um recurso destaca o mesmo id nos dois
painéis; o painel lateral mostra os dois lados e os fatos que diferem. Uma tabela de cobertura
abaixo dos painéis tem uma linha por namespace. Clicar numa linha liga o filtro de namespace
(`ns=<namespace>` no hash da URL, `_` para escopo de cluster): os dois painéis e os setores ficam só
com os recursos cujo namespace é exatamente aquele, a linha fica destacada e um chip no cabeçalho
mostra o filtro. Clicar de novo na mesma linha, ou no chip, desliga o filtro. O filtro é
independente da busca (`q`), que continua esmaecendo o que não casa. O rodapé conta cada
identidade uma vez nos dois painéis; as contagens por estado seguem os filtros ativos (categorias,
sistema, namespace), e a cobertura é o total de todos os namespaces fora os de sistema, o mesmo
total da CLI.

## CLI e TUI

Fang v2 sobre Cobra nos comandos; Bubble Tea v2, Bubbles v2, Lip Gloss v2 e Huh na TUI.
`--format text|json` em todo comando.

| Comando | Faz |
|---|---|
| `kubestrata` | TUI: contextos, idade do cache, status; `r` refresh, `g` gerar, `c` comparar, `o` abrir |
| `kubestrata contexts` | contextos do kubeconfig (sem acessar cluster) |
| `kubestrata collect [-c ctx]... [--all] [--max-age 6h] [--force]` | grava no cache; reaproveita snapshot mais novo que `--max-age` |
| `kubestrata render [-c ctx] [--scale 5d\|10d\|30d\|log] [--axis created\|changed] [-o dir] [--open]` | site a partir do cache |
| `kubestrata compare -c source -c target [--scale ...] [--axis ...] [-o dir] [--open]` | tabela de cobertura no stdout e site lado a lado a partir do cache |
| `kubestrata cache ls\|prune [--keep 5]` | manutenção dos snapshots |

O estado da página fica no hash da URL
(`#scale=log&axis=changed&color=manager&ns=<namespace>&sel=<id>`), então uma visão pode virar
favorito e ser conferida em modo headless; o `ns=` também funciona na visão de um cluster. Uma
Content-Security-Policy (`default-src 'none'`) mantém a página fora da rede. Depois do primeiro
desenho a página grava `data-status`, `data-shown` (recursos desenhados, somados nos painéis),
`data-draw-ms` e `data-total-ms` no `<body>` para checagem automatizada. O `make page-check` grava
os dois sites a partir do cache, carrega as páginas no Chrome headless em vários estados de hash e
falha se alguma página não informar `data-status="ok"`. Medido no Chrome headless num M4: 20 000
recursos desenham em cerca de 10–16 ms (primeiro desenho em cerca de 50 ms, arquivo de dados de
4,8 MB).

Saída do site (padrão `<diretório de cache>/site` no `render`, `<diretório de cache>/compare` no
`compare`): `index.html` (CSS e JS inline, sem CDN), `data/<cluster_id>.js`
(`window.KUBESTRATA["<id>"] = {...}`; tag script, porque o Chrome bloqueia `fetch()` de JSON local
em `file://`), `json/<cluster_id>.json` para máquina; o `compare` acrescenta `data/compare.js`
(`window.KUBESTRATA_COMPARE = {...}`) e `json/compare.json`.

## Fases

| Fase | Escopo | Pronto quando |
|---|---|---|
| F1 (v0.1.0) | contexts, collect + cache, render, compare, TUI básica, saída JSON | e2e verde em dois clusters locais; HTML golden idêntico entre execuções; teste de não vazamento verde; página sem erro de JS no Chrome headless; 20k nós com pan e zoom medidos |
| F2 | unidades de migração, classificação (manager, estado, exposição, acoplamento à nuvem, fim de vida), `plan --format json\|csv\|md`, resumo por time, `--map-ns` | plano do fixture bate com uma verdade-base fixa |
| F3 | diff temporal entre snapshots, checagem de imagem multi-arch, `kubectl strata` via Krew | decidido quando a F2 estiver em uso real |
