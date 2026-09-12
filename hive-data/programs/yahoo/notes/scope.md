---
program_id: yahoo
document_type: rules
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [root, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# Escopo — Yahoo (Paranoids)

Fonte: Intigriti — programa Yahoo (público) · Última conferência: 2026-09-08

> Wildcards da Yahoo são **por token** (o sub precisa CONTER o token, ex. `*omega*.yahoo.com`).
> O matcher do `scope_hook` só entende o wildcard-prefixo `*.base`; portanto os wildcards
> por-token abaixo servem de referência humana, e o **enforcement duro é a lista Out of scope**
> (deny-wins). Hosts desconhecidos passam pelo hook — a régua real continua sendo esta ficha.

## In scope

Wildcards (por token — sub deve conter o token):

- `*ensemble*.yahoo.com`
- `*omega*.yahoo.com`

URLs/hosts exatos:

- `apis.mail.yahoo.com`
- `data.mail.yahoo.com`
- `onepush.query.yahoo.com`
- `proddata.xobni.yahoo.com`
- `yimg.com`

Produtos Yahoo (Other, Tier 2 — hosts próprios sob `yahoo.com` e afins listados na política):
Mail · Finance · Search · News · Calendar · Weather · Video · Sports (Fantasy / Fantasy Wallet /
Daily Fantasy / PicknWin / Best Ball / Editorial / Mobile) · eCommerce TW · TW Media · Yahoo HK
News · Gemini · Identity Services · Low Cost Access · 7 News · Social Media Accounts · Open Source
Projects · Yahoo! (Misc). Política: vuln em asset Yahoo não-listado ainda pode valer — reportar.

SSRF testbed autorizado ("bananastand", CWE-918 — alvos dados pela própria Yahoo):

- `banana.stand.gq1.yahoo.com`
- `banana.stand.bf1.yahoo.com`
- `banana.stand.bf2.yahoo.com`
- `banana.stand.ne1.yahoo.com`
- `banana.stand.sg3.yahoo.com`
- `banana.stand.ir2.yahoo.com`
- `banana.stand.tw1.yahoo.com`
- `banana.stand.tp2.yahoo.com`

## Out of scope

- `*.yahoo.net`
- `*.yahoo.com.tw`
- `*.yahoo.com.hk`
- `*.yahooinc.com`
- `*.vdms.com`
- `*.verizondigitalmedia.com`
- `ssp.verizonmedia.com`
- `adaptv.advertising.com`
- `alephd.com`
- `vidible.tv`
- `store.vzbuilders.com`
- `files.molo.ch`
- `artifact.news`
- `vetted.ai`
- `*.tumblr.com`
- `*.flickr.com`
- `*.aol.com`
- `*.engadget.com`
- `*.techcrunch.com`
- `*.mapquest.com`

Classes fora de escopo / sem bounty (não são hosts — referência humana):
DoS / brute force · produtos vendidos/EOL (Tumblr, Flickr, AOL, Engadget, TechCrunch, MapQuest,
Yahoo Answers/Groups/Messenger, Yahoo Japan) · Cricket · Factual · Wagr · CommonStock ·
self-XSS · clickjacking · missing headers · open redirect intencional · CSRF de login/logout ·
resultados de scanner · enumeração de conta · version disclosure.

⚠️ Nuance: "TW eCommerce" é asset in-scope, MAS `*.yahoo.com.tw` é OOS — confirmar o domínio
real do produto (host próprio sob `yahoo.com`) antes de tocar.

## Regras relevantes

- Rate máximo: 50 req/s (finance.yahoo.com dá 429/bot-mgmt rápido — manter volume mínimo).
- Header obrigatório em TODO tráfego ativo: X-Bug-Bounty: Intigriti-moldret.
- IP incluído no report (eles cruzam com logs).
- Anexos só no report — nada de host externo (YouTube/Vimeo/S3).
- Não pivotar / não exfiltrar — PoC mínima; dado sensível só p/ validar e devolver.
- Ceasefire: blackout ~90d por classe se postarem.
- Disclosure só com permissão escrita.
- Conta de teste ativa: moldret_intigriti@yahoo.com (autorizada 2026-09-07).

## Histórico de mudanças de escopo

| Data       | Mudança                                                        |
|------------|----------------------------------------------------------------|
| 2026-09-08 | scope.md preenchido a partir da ficha (era template exemplo).  |
