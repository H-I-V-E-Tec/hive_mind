---
program_id: etoro
document_type: evidence
classification: internal
claimed_scope_status: unknown
source: excalibull/targets/etoro/findings/ssrf-edge-waf-bypass.md
collected_at: 2026-09-09T13:47:24Z
tags: [excalibull, import, finding, ssrf, waf-bypass]
asset_refs: [www.etoro.com]
---

# SSRF response-based no fetcher de URL — filtro de borda (edge WAF) contornável

| Campo      | Valor                                  |
|------------|----------------------------------------|
| Alvo       | bugcrowd/etoro                          |
| Data       | 2026-09-09                              |
| Severidade | High (a elevar p/ Critical se metadata/cred interna) |
| CVSS       | a calcular após confirmar alcance interno |
| Endpoint   | `GET {BASE}/api/edm-streams/v1/attachment?url=<urlencoded>` (fetcher de anexo EDM) |
| Status     | rascunho / em investigação              |
| Report ID  | —                                      |
| Recompensa | —                                      |
| Autoria    | Tiago + amigo                           |

## Resumo

O app expõe um fetcher de URL que busca um recurso arbitrário e **reflete** `title`,
`host` e `description` do recurso na resposta (SSRF **não-cego / response-based**). Não há
allow-list no app. A **única** proteção é o edge WAF (Cloudflare/GCP), que filtra apenas a
**string literal de IP interno**. Variações de `localhost`/loopback passam pela borda e
chegam ao fetcher: a resposta vem como **JSON do APP** (e não o 403 do edge), provando que
o app resolveu e processou uma URL apontada pro loopback/rede interna.

## Por que funciona (mecânica — cérebro)

- **P5 — Parser A ≠ Parser B:** o WAF valida a *string* da URL (bloqueia o literal do IP
  interno), mas o *fetcher* do app normaliza/resolve de forma diferente e busca mesmo assim.
  Validação vê X, execução usa Y.
- **P8 — bypass por variação de request:** o controle casa com uma forma específica
  (o literal) e falha nas variantes de loopback.
- Sinal reflete-metadados (`title`/`host`/`description`) ⇒ canal de **leitura**: dá pra
  exfiltrar conteúdo de serviços internos, não só "bater e voltar" (não é SSRF só-DNS).

## Passos para reproduzir (a preencher com a requisição exata)

1. `CONFIRMAR` — requisição ao fetcher com URL externa arbitrária → app reflete title/host/description (baseline: SSRF sem allow-list).
2. `CONFIRMAR` — mesma requisição com o **literal** de IP interno → **403 do edge** (WAF).
3. `CONFIRMAR` — trocar pelo variante de loopback/localhost que passou → **JSON do APP** (bypass confirmado).

## PoC

```
<a preencher: requisição + resposta redigidas — SEM tokens/PII;
 guardar as 3 variantes: externo OK, literal bloqueado (403 edge), variante interna (JSON app)>
```

## Impacto

SSRF com filtro de borda contornado e canal de leitura (reflexão de metadados). Impacto
final depende do que o loopback/rede interna alcança — ver plano de escalada abaixo.

## Correção sugerida

Guard **no app** (não confiar só no edge): resolver o hostname e bloquear por **IP de
destino** após resolução (loopback `127/8` + `::1`, RFC1918, link-local `169.254/16`,
`0.0.0.0`, IPv6 mapeado), negar redirects pra faixas internas, allow-list de destinos,
e proibir esquemas não-http(s).

## Linha do tempo

| Data       | Evento                                  |
|------------|-----------------------------------------|
| 2026-09-09 | descoberto (Tiago + amigo); bypass do edge confirmado (JSON do app no loopback) |

---

## Escada de escalada (chain-table → SSRF) — do "só-DNS" ao pagável

> **never-submit:** SSRF só-DNS (bater e voltar, sem alcance interno) **não se envia**.
> Como o app **reflete** o corpo (title/host/description), temos canal de LEITURA — o
> objetivo é subir a escada até provar leitura de algo interno/sensível.

Ordem por impacto (chain-table.md §SSRF):

1. **Metadata cloud → credenciais IAM (Acesso cloud) — CRÍTICO.**
   eToro tem infra cloud (context.txt: Vercel/Azure B2C/S3). Testar o IMDS do provedor:
   - AWS: `http://169.254.169.254/latest/meta-data/` (e `/iam/security-credentials/`).
   - GCP: `http://metadata.google.internal/computeMetadata/v1/` (exige header `Metadata-Flavor`; só dá se o fetcher deixar setar header — geralmente não).
   - Azure: `http://169.254.169.254/metadata/instance?api-version=...` (exige `Metadata:true`).
   Se o title/description refletir chave/role → **Critical**.
2. **Serviços internos → painéis/admin (Acesso interno).** Varrer portas/hosts internos
   comuns via reflexão (o title revela o serviço): `127.0.0.1:{80,8080,8000,9200,6379,5601,15672,8500}`,
   nomes internos (`*.internal`, `*.consul`, `kubernetes.default`).
3. **localhost → bypass de allow-list de IP (Bypass de auth).** Serviços que confiam em
   loopback (actuator, admin sem auth ligado a 127.0.0.1).

## Plano de teste (respeitando postura escudo + delegação)

- **Quem roda:** o próprio fetch já é ação ATIVA c/ payload → **o Tiago executa**; Claude
  desenha a requisição e interpreta. Header `X-Bug-Bounty: moldret` em TODA chamada.
- **Baixo volume, serial, sem fuzz** (postura escudo proíbe varredura). Uma variante por vez.
- **Matriz de bypass do edge** (documentar a resposta de cada: 403-edge vs JSON-app):
  loopback decimal (`2130706433`), octal (`0177.0.0.1`), hex (`0x7f000001`), `0.0.0.0`,
  `[::1]`/`[::ffff:127.0.0.1]`, `127.0.0.1.nip.io`, `localtest.me`, e o wrap que JÁ passou.
- **Prova de leitura interna:** apontar pra um serviço interno que devolva corpo distinto
  e confirmar que o `title/description` reflete o conteúdo (não só status).
- **DNS-rebind / redirect** só se o filtro do app resolver-e-checar (P5): host do atacante
  que responde 302→interno, ou TTL-0 rebind, para contornar guard pós-resolução.

## Perguntas abertas (pra fechar o finding)

1. **Endpoint exato do fetcher** (host + método + parâmetro que recebe a URL)?
2. Qual foi o **wrap de loopback** que passou o edge (o literal do bypass)?
3. Já alcançou algum **serviço interno com corpo** ou só loopback "vazio"?
4. Cloud provider do host que faz o fetch (define qual IMDS testar)?
