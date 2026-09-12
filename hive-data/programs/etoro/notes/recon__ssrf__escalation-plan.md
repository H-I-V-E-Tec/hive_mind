---
program_id: etoro
document_type: note
claimed_scope_status: unknown
classification: internal
source: bugcrowd policy (etoro) — 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [etoro, recon]
asset_refs: [etoro.com]
---
# Escada de escalada — SSRF edm-streams (PLANO hipotético)

> **Premissa assumida (NÃO confirmada):** o `reach_test.py` retornou `APP-JSON` num alvo
> loopback ⇒ o app fetchou loopback ⇒ WAF (edge) burlado, **reach confirmado**.
> Este doc é PLANO ("se X, então Y"), não achado. Execução autenticada = Tiago roda.
> Header `X-Bug-Bounty: moldret` sempre. Postura escudo: serial, 1 variante/vez, sem
> varredura de volume (lista curada, NÃO fuzz de porta — sweep = scan proibido).

Endpoint: `GET www.etoro.com/api/edm-streams/v1/attachment?url=<BYPASS>`
Canal: o fetcher parseia o recurso e **reflete `title`/`host`/`description`** → canal de LEITURA.
`<BYPASS>` = o wrap de loopback/encoding que passou o edge (do reach_test).

---

## Degrau 0 — ONDE ESTAMOS (reach-only)  ❌ não-submetível
Loopback alcançado, mas sem prova de corpo interno lido. `never-submit`: SSRF só-DNS/reach
não envia. Precisa subir ≥1 degrau.

## Degrau 1 — provar LEITURA de corpo interno  ✅ cruza o gate (Data breach)
**Objetivo:** confirmar que a reflexão carrega o **CORPO** do serviço interno, não só status.
**Como:** apontar pra um serviço interno que devolva corpo distinto e ver `title`/`description`
refletir o conteúdo. Lista curada (serial), casada com a stack eToro:
- `127.0.0.1:3000` (Next/Vercel), `:80/:8080` (BFF/proxy), `:9200` (Elasticsearch → JSON com
  `cluster_name`/`version` no corpo = prova limpa e não-sensível), `:6379` (redis), `:8500`
  (consul), `:15672` (rabbit mgmt).
**Sinal de sucesso:** `title`/`description` refletindo string única do corpo interno
(ex.: `"You Know, for Search"` do ES, ou o cluster_name).
**Se sucesso:** já é reportável (SSRF interno com leitura). Segue subindo se der Critical.
**Gate ético:** ES/consul → provar com metadado do serviço (version/cluster), **não** dumpar
índice/PII. Corpo mínimo pra prova.

## Degrau 2 — cloud metadata → credencial IAM  🔴 CRÍTICO (terminal, pare e reporte)
**Objetivo:** IMDS refletido = roubo de credencial de máquina.
**Nuance de header:** o fetcher NÃO deixa setar header custom (é só `?url=`), então:
- **GCP** (`metadata.google.internal`, exige `Metadata-Flavor`) → provável **bloqueado**.
- **Azure** (`169.254.169.254/metadata`, exige `Metadata:true`) → provável **bloqueado**.
- **AWS IMDSv1** (sem header) → **é o candidato real** SE o host não força IMDSv2.
**Bypass do IP no edge:** `169.254.169.254` literal cai no WAF → usar encoding
(decimal `2852039166`, hex `0xa9fea9fe`) no mesmo esquema do `<BYPASS>`.
**Requisições (uma por vez):**
1. `.../attachment?url=http://2852039166/latest/meta-data/` → título lista metadata? (IMDSv1 vivo)
2. `.../attachment?url=http://2852039166/latest/meta-data/iam/security-credentials/` → nome da role
3. (só se 1–2 refletirem) `.../iam/security-credentials/<ROLE>` → **AccessKey/SecretKey/Token**
**Sinal:** `title`/`description` refletindo `AccessKeyId`/`Token` ⇒ **Critical**, PARE (impacto
terminal, chain-table). **Redija a chave real no report** — prova = os campos existirem.

## Degrau 3 — serviços internos / painéis admin  🟠 (Acesso interno)
Se IMDS não der, mapear hosts internos via reflexão (curado, não sweep): `*.internal`,
`kubernetes.default`, nomes da stack (oauth.wallet interno, cashier .NET, webhooks de
custódia). Painel/admin sem auth ligado a loopback refletido → High/Critical conforme o corpo.

## Degrau 4 — guard pós-resolução (P5) via redirect/rebind  (só se necessário)
Se o app resolver-e-checar o IP (não só a string): host do atacante que responde
`302 → alvo interno`, ou DNS-rebind TTL-0. Contorna guard que valida antes de fetchar.
Requer infra externa (domínio + servidor) — desenhar só se Degrau 1–3 baterem no guard.

---

## Ordem de execução (mínimo ruído, máximo impacto)
1. Degrau 1 no ES/serviço com corpo → **prova de leitura** (destrava o report).
2. Degrau 2 AWS IMDSv1 → tentativa de **Critical** (2–3 requests, serial).
3. Degrau 3 se 2 falhar. Degrau 4 só se houver guard.

## Gates (não furar)
- Autenticado = Tiago roda; Claude desenha/interpreta.
- Escudo: serial, `sleep`, lista curada — **nada de varrer 1000 portas** (= scan proibido).
- Corpo mínimo de prova; **nunca** dumpar PII/segredo real além do necessário; redigir no report.
- Se bater `Critical` (IMDS/cred) → PARE de chainar, reporte (impacto terminal).
