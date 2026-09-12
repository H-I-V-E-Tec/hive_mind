---
program_id: etoro
document_type: rules
claimed_scope_status: unknown
classification: internal
source: bugcrowd policy (etoro) — 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [etoro, root]
asset_refs: [etoro.com]
---
# Escopo — eToro (Bugcrowd)

Fonte: https://bugcrowd.com/etoro · Última conferência: 2026-09-09

## In scope

| Asset | Tipo | Observações |
|-------|------|-------------|
| `*.etoro.com` | wildcard | fintech; recon nível 3 |
| `etorox.com` | domínio | in-scope |
| `etoropartners.com` | domínio | in-scope |
| `delta.app` | domínio | in-scope |

## Out of scope / proibido (política + postura)

| Asset / classe | Observações |
|----------------|-------------|
| Scan de volume, fuzz, brute force | proibido |
| Tocar conta de terceiro | proibido |
| Payload de injeção / abuso automatizado | proibido |
| DoS / stress | proibido |

## Regras relevantes

- **Header obrigatório em TODO tráfego:** `X-Bug-Bounty: moldret` (handle Bugcrowd = `moldret`).
- Podem pedir registro de IP/UA/username. IP de saída: `177.40.95.165`.
- Chamada **autenticada** → **o Tiago roda** (regra de delegação); Claude desenha + interpreta.
- GET anônimo/público de baixo volume, serial, parsing offline → Claude pode pedir direto.
- **Postura escudo** ativa: só o que não dispara flag (WAF/rate/blue team). Lança desligada.

## Histórico de mudanças de escopo

| Data       | Mudança                          |
|------------|----------------------------------|
| 2026-09-09 | ficha sincronizada do board (bugcrowd/etoro); SSRF em investigação |
