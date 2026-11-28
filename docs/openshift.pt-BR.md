# Primeira execução no OpenShift

[English](openshift.md)

Este guia leva o kubestrata por um primeiro inventário de um cluster OpenShift 4 e depois por uma
comparação com o cluster para onde você migra. Todos os exemplos usam marcadores: `ocp` para o
contexto do OpenShift, `eks` para o destino, `api.<cluster>.<dominio-base>` para o host da API.

O kubestrata lê o cluster só com `list` e `get` e guarda o que coleta num cache local
(`~/Library/Caches/kubestrata` no macOS, `~/.cache/kubestrata` no Linux). Nada é enviado para lugar
nenhum.

## 1. Entre e confira o contexto

Entre com a CA do cluster em vez de `--insecure-skip-tls-verify`. O bundle de CA do certificado de
serviço do API server vem do administrador do cluster; confira a impressão digital por outro canal.

```sh
oc login --certificate-authority=ca.crt https://api.<cluster>.<dominio-base>:6443
kubestrata contexts
```

O `oc login` dá ao contexto o nome `<namespace>/api-<cluster>-<dominio-base>:6443/<usuário>`.
Renomeie para algo curto, porque o kubestrata indexa o cache pelo nome do contexto:

```sh
kubectl config rename-context "$(kubectl config current-context)" ocp
```

A coluna `NOTES` do `kubestrata contexts` marca um cluster que pula a verificação de TLS e um
usuário com token estático. O token de um `oc login` é um access token OAuth: é estático para o
kubeconfig, então saia quando terminar (passo 8). O README, em "Higiene do kubeconfig", tem os
detalhes.

## 2. Escolha as permissões

Um administrador do cluster pode criar uma ServiceAccount só de leitura para o inventário, para a
coleta não rodar com um token pessoal:

| Arquivo | Para |
|---|---|
| [`deploy/rbac/kubestrata-reader.yaml`](../deploy/rbac/kubestrata-reader.yaml) | `list` em todo kind, Secrets inclusive (como metadata) |
| [`deploy/rbac/kubestrata-reader-no-secrets.yaml`](../deploy/rbac/kubestrata-reader-no-secrets.yaml) | os kinds do inventário, os grupos de API do OpenShift e custom resources comuns, nunca Secrets: colete com `--no-secrets` |

Sem Secrets o inventário perde o estado das releases Helm e as ligações de workloads para Secrets;
o Helm continua detectado pelos labels dos objetos. O README, em "Permissões", mostra como montar
um kubeconfig com um token de vida curta da ServiceAccount.

## 3. Colete

```sh
kubestrata collect -c ocp --no-secrets          # ou sem --no-secrets com o kubestrata-reader
kubestrata collect -c ocp --no-secrets --team-from-ns '^([a-z]+)-(dev|test|prod)$'
```

A coluna `TEAMS` mostra quantos namespaces têm time. O kubestrata tira o time de um namespace dos
labels e annotations (`team`, `owner`, `app.kubernetes.io/part-of`), depois do nome quando você
passa `--team-from-ns` (o primeiro grupo da regexp é o time), depois do `openshift.io/requester`, o
usuário que pediu o projeto, a não ser que esse usuário seja `kube:admin`, `system:*`, `admin` ou um
do `--requester-ignore`. Um requester é um nome de usuário: trate como dado pessoal.

Os namespaces que casam com `openshift-*`, `kube-*` e os prefixos dos provedores de nuvem são de
sistema: são coletados, ficam ocultos por padrão na página e ficam fora dos totais.

## 4. Olhe

Um cluster de vida longa mantém a maior parte dos recursos com mais de um ano. A escala padrão
`30d` põe esses recursos na faixa externa; use anéis em anos, ou `auto` (anéis nos quantis das
idades):

```sh
kubestrata render -c ocp --scale years --open
kubestrata render -c ocp --rings 30d,90d,180d,1y,2y,3y,5y --open
kubestrata render -c ocp --scale auto --open
```

O que a página mostra para o OpenShift:

| Você vê | Onde |
|---|---|
| Routes | categoria network, com host, path, terminação TLS e Service de backend no painel de detalhe |
| DeploymentConfigs | categoria workload, com os ReplicationControllers e pods agregados, e `end_of_life`: depreciado desde o OpenShift 4.14, destino Deployment |
| Ingresses do ingress-nginx, StorageClasses in-tree | `end_of_life` com o substituto |
| MachineConfig, MachineConfigPool, APIRequestCount, configs dos cluster operators, a Machine API | a categoria `platform`, oculta por padrão: clique no chip dela ou acrescente `show=platform` na URL |
| as ClusterServiceVersions que o OLM copia em todo namespace | fundidas na original, que as conta (`copies`) |
| Argo CD (OpenShift GitOps) | manager `argocd`, pelos labels e annotations ou pelo `status.resources` das Applications; a visão geral avisa quando o Argo CD está instalado mas não gerencia nada do que foi coletado |
| edições feitas no console web | manager `console`: uma mudança manual para codificar antes de migrar |
| Carvel kapp | manager `kapp` |

A busca aceita vários valores: `ns:payments,ns:checkout kind:DeploymentConfig`, `-mgr:platform`.

Para os números de que um plano de migração parte, em Markdown:

```sh
kubestrata summary -c ocp > ocp-summary.md
```

## 5. Compartilhe sem vazar a identidade do cluster

O `--redact` mascara, em toda saída, no arquivo do `--snapshot-out` e no site: a parte
`<cluster>.<dominio-base>` dos hosts `api.`, `api-int.` e `*.apps.` (`api.redacted:6443`,
`web.apps.redacted`), o host da API com hífens nos nomes de contexto do `oc login` e os nomes de
time tirados do `openshift.io/requester` (`redacted-1`, `redacted-2`). Ele mantém namespaces, nomes
de recursos, imagens e domínios custom de Route: revise um site antes de publicar.

```sh
kubestrata render -c ocp --redact -o ./ocp-site
kubestrata collect -c ocp --snapshot-out ocp.json --redact
```

## 6. Compare com o destino

Colete o destino com o contexto dele e compare:

```sh
kubestrata collect -c eks
kubestrata compare -c ocp -c eks --open
```

O `compare` pareia recursos por `group/Kind/namespace/name` e depois pareia os kinds do OpenShift
com o que o destino serve no lugar, no mesmo namespace: uma Route com uma HTTPRoute ou um Ingress
(um host do destino é o host da Route, o mesmo nome primeiro, senão o mesmo nome) e uma
DeploymentConfig com o Deployment de mesmo nome. Esses pares ficam equal ou drift nos fatos que os
dois kinds expressam (host, path, TLS, Service e porta de backend; imagens, réplicas e o resto do
template de pod), contam como cobertos e mostram a tradução (`Route→HTTPRoute`) no painel de
detalhe. Um namespace renomeado no destino ainda não forma par.

## 7. Acompanhe um cluster no tempo

```sh
kubestrata collect -c ocp --force      # de novo, mais tarde
kubestrata diff -c ocp                 # o que apareceu, sumiu ou mudou
```

## 8. Limpe

```sh
kubectl delete -f deploy/rbac/kubestrata-reader-no-secrets.yaml   # se você criou a ServiceAccount
oc logout                                                       # apaga o token OAuth no servidor
kubestrata cache ls                                             # os snapshots que você guarda
kubestrata cache prune --keep 1
```

O cache e os sites descrevem o cluster: mantenha esses arquivos fora do git e de drives
compartilhados.
