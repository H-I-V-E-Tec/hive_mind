---
program_id: coinspot
document_type: note
claimed_scope_status: unknown
classification: internal
source: hackerone policy (coinspot) — CSV export 2026-09-09
collected_at: 2026-09-12T00:00:00Z
tags: [coinspot, root]
asset_refs: [www.coinspot.com.au]
---
# A caçada até aqui — CoinSpot

Log de progresso. A **ficha** (`README.md`) é o estado destilado; aqui é a narrativa + próximos passos.

---

## Sessão 2026-09-09 — `modo hunter` (nível 1 destravado, recon passivo)

### O que rodou
- `modo hunter on` → alvo coinspot. `code 7` (board diff), `code 8` (brain brief).
- **Escopo reconferido contra a policy viva:** CSV oficial de scopes do H1 (2026-09-09 10:27 UTC). 4 assets — `www.coinspot.com.au` + `/v2/api` = **critical**, apps mobile = high. **Sem mudança material** vs. 2026-09-02. Apex sem `www` saiu da lista (só redirect). `scope.md` atualizado + corrigido bug de parser do `scope_hook` (asset entre crases abaixo de "Out of scope" virava regra OOS e bloqueava tudo).
- **Nível 1 destravado (parcial):** Tiago forneceu **1 API key read-only** + 1 conta de teste (alias wearehackerone, sem KYC). Gravada via `code -1` em `$SCRATCHPAD/coinspot_session.env` (chmod 600, fora do repo).
- Harness `cs.py` no scratchpad: assina HMAC-SHA512 sobre JSON cru byte-idêntico ao body, nonce `int(time·1e6)`, permite body cru / nonce custom / assinar-A-enviar-B / V1. Baseline: `POST /api/v2/ro/my/balances` → **HTTP 200**, conta com ~0.000185 BTC (~$20.60 AUD) real.
- **Recon passivo (a pedido do Tiago, antes de tocar a API autenticada):**
  - `urls.txt` (119k): **zero** secret/token/JWT vazado. Só NFT marketplace + `/charts/*` (público) + refs/affiliates públicos. Novas rotas auth-gated pro inventário: `/my/instantbuy` `/my/instantsell` `/my/buybundle` `/my/bundleconfirmation` `/my/cspaymentcheckout_reconnect` `/my/depositissue` `/my/wallet/{coin}/receive` `/my/btcorders/new` `/charts/config`.
  - Doc da API v2 + 2 wrappers (`geekpete/py-coinspot-api`, `jamesbuch/coinspot-python-api`): **confirmam P13** — `sign = HMAC-SHA512(secret, json.dumps(postdata, separators=(",",":")))`, byte-idêntico ao body. Premissa da **C1 (dup-key smuggling)** sólida. Novos params: `/my/coin/withdraw/send` tem opcionais **`paymentid`** + **`network`**; `/my/balance/{coin}` tem `available`. Endpoints `/api/v2/status` e `/api/v2/ro/status` (access-check) — bons pro lead D.
  - APK `com.coinspot.app`: **ainda bloqueado** — sem `jadx`/`apktool`/`java`/`node` local; mirrors deram 403/404 antes. GitHub code-search (C3) exige login; `gh` não instalado. C3 rebaixada pela policy de qualquer forma.

### `formação de lança` — leads B / D / C2 (key RO, decisão do Tiago: rodar já)

- **Lead D — escopo RO→write / V1 = NEGATIVO.** Check de tipo de key **antes do dispatch**, em V1 e V2: `/api/v2/status`, `/my/coin/deposit`, `/my/buy/cancel`, V1 `/api/my/balances` → todos `401 "key type unauthorised for this request"`. A key V2 autentica na V1 mas só na base `/api/ro` (ainda read-only). Sem bypass.
- **Lead C2 — replay cross-versão = NEGATIVO.** Contador de `nonce` é **global por API key**: compartilhado V1↔V2, entre bases e entre todos os endpoints. `nonce = agora−5s` → `401 "invalid nonce"` em V1 mesmo após só a V2 ter visto o valor alto. Assinatura **é** portável V1↔V2 (sign reusado passou pro check de nonce), mas o contador único é o backstop. P6/P19 mortos p/ replay.
- **Lead B — diferencial de parser do `nonce` = NEGATIVO.** Com a key: o `nonce` é `parseInt`-leniente (aceita `"T"`, `" T "`, `"Tabc"`, `"000T"` → 200), **mas** o store guarda o valor **numérico** e a comparação é numérica — um int puro maior depois passa (sem replay, sem DoS lexicográfico). `-1` → `invalid nonce`; `[T]` → `400 invalid data`.
- **🚫 CHAIN ⭐ C1 (dup-key smuggling no saque) — REFUTADA na mecânica.** Provado por probes *assino-A-envio-B* em `/api/v2/ro/status`:
  - **H1:** assino `{"nonce":T}` canônico, envio `{"nonce": T }` com espaços → **200**.
  - **H4:** assino+envio idêntico *com* espaço → **401 failed signature**.
  - **K1:** envio `{"a":1,"a":2}`, assino colapsado `{"a":2}` → **200**.
  - **K3/K4:** dup-key `{"z":"A","z":"B"}` → assinar `{"z":"B"}` (último) passa, `{"z":"A"}` (primeiro) falha.
  - ⇒ o server faz **um** parse (last-key-wins, resolve `\uXXXX` em chaves), **re-serializa compacto** e HMACa o **canônico** — **não** os bytes crus. O handler usa o mesmo objeto parseado. Dup-key colapsa **antes** do check de assinatura; o atacante é obrigado a assinar a forma já colapsada = o que o handler executa. **Sem 2º parse divergente.** P2+P13 mortos p/ este fim.

### `formação de lança` (cont.) — key full/trade (withdraw NÃO habilitado)

- Tiago forneceu **key full/trade** (2ª key, `COINSPOT_FULL_API_*` no env). `/api/v2/status` → 200. **`/my/coin/withdraw/senddetails` → 401 "coin withdrawals have not been enabled for this API key"** — falta ligar o sub-toggle de withdraw. **F/P7/C3 seguem bloqueados.**
- **Quote tampering (`/quote/{buy,sell,swap}/now`) = NEGATIVO.** Bem validados: `amount` negativo/zero → 400 `Valid amount required`; `amounttype` inválido → 400; swap same-coin → 400 `Coin required`. `amount` gigante retorna só um `rate` (sem ordem).
- **`/api/v2/my/coin/deposit` — oddity INCONCLUSIVA (nota, não finding):** 1ª chamada `{cointype:BTC}` → só endereço Bitcoin. Após chamar com `{cointype:USDT}` ou `{cointype:BTC,network:ETH}`, **toda** chamada seguinte (BTC/LTC/DOGE/ADA/XRP, até coins nunca consultadas) passa a incluir uma entrada `name:"BNB Smart Chain"` com **`network:"ETH"`** (mislabel — chains distintas) e o endereço EVM `0x8067…` da conta. **Estado persiste.** Hipótese: suporte Binance-Peg *by-design* + label cosmético errado → se um cliente/UI chaveia por `network` mostra "ETH" e o user deposita na chain errada = false top-up/perda (rubric **High**). **Não provável sem observar crédito de depósito real** (fora do acesso/seguro). Provável by-design.

### `descobrir a roda` + `armar a arapuca` (2026-09-09) — modelo do sistema + chains novas
- Reconstruí o modelo da API v2 (seção "Modelo do sistema" em `arapuca.md`): assinatura = HMAC do canônico re-parseado (P20), nonce = contador global por key (P21), escopo pré-dispatch (P22), `/my/coin/deposit` muta estado (P23), key V2 vale na V1 (P24). 10 fronteiras de confiança mapeadas (B1–B10), 9 hipóteses novas (H-A…H-I), 5 chains novas (C5–C9).

### `formação de lança` (cont.) — chains C5 e C6 (key full/trade)
- **C5 — race no nonce = NEGATIVO.** Burst de 30 requests idênticas (mesmo nonce+sign+body) a `/api/v2/ro/status`, 2 rodadas: **exatamente 1 → 200, 29 → 401 `invalid nonce`**. Gate check-then-store **atômico/serializado**. Mata C5, lead C, ângulo race da P14.
- **C6 (limit-path) + lead G = NEGATIVO** (ordens-sonda reais, conta teste, fundos simulados, autorizado):
  - `/my/sell` BTC 0.00001 @ `rate=220000` (2× mercado ~110k) → ordem criada, **não preencheu, ZERO AUD** → `rate` é **limite/proteção**, não preço de execução. Motor usa preço real. C6 limit-path refutada.
  - **H-I não diverge:** `amount` string `"0.00001"` e float `0.00001` ambos aceitos e normalizados a `1e-05` server-side no path de ordem.
  - **Lead G/H-E:** `/my/sell/edit` valida `rate` submetido vs atual (`Rate to edit not matched - submitted rate:X, actual rate:Y`), `newrate` 0/negativo → 400. Fail-closed. (Erro vaza o `rate` atual da **própria** ordem — trivial.)
  - Ordens via `/my/sell` aparecem em `ro/my/orders/**market**/open` (não `limit/open`) — terminologia, não bug.
  - **Higiene:** 2 ordens canceladas, book limpo, saldo 0.000185 BTC intacto.

### Resultado
- **Auth da API v2 = sólida.** Tudo negativo: B, C2, C5, D, quote, C6-limit, G. C1 refutada. Deposit oddity (C9) inconclusiva/`dormant`.
- **Superfície que sobra** precisa de mais acesso (toggle withdraw p/ F/P7/H-G; 2ª conta p/ IDOR C8), não de mais engenhosidade.

### Selo de saída — 2026-09-09 (`modo hunter off`)
- **Modo hunter + formação de lança DESLIGADOS.** Nível 1 (1 conta) da API v2 exaurido na camada de auth/lógica sem achado. `arapuca.md` com modelo do sistema + C5–C9. Brain com todos os vereditos.
- **Bloqueios por lead quente:**
  - **F / P7 / H-G** (saque sem 2FA · network confusion no `send`): key full **NÃO** tem withdraw habilitado (`"coin withdrawals have not been enabled for this API key"`) → **bloqueado até o Tiago ligar o toggle de withdraw** (confirmação por e-mail).
  - **C8** (IDOR web `orders/eofy/{year}`, `buycomplete/{ObjectId}` — fit #1 do programa, intocado): **precisa 2ª conta de teste + cookie web** (`code -1` com Copy-as-cURL).
  - **C6 instant-path** (`/my/sell/now` rate=execução?): **não testado — requer OK explícito** (executa na hora). Limit-path já refutado.
  - **C9** (deposit chain-confusion): `dormant` — só avança observando crédito de depósito real.
- **Segredos:** 2 keys (RO + full) e nenhum cookie ficaram só em `$SCRATCHPAD/coinspot_session.env` (fora do repo, chmod 600); some ao fim da sessão.

### Próximo passo
- **Destravar mais acesso, não mais teste:** (1) ligar o **toggle de withdraw** na key full → retomar por **F/P7/H-G** (saque; erro-diferencial, sem send real); (2) criar **2ª conta + key RO** e colar **cookie web** → **C8** IDOR cross-account (maior fit do programa). Opcional: OK no **C6 instant-path** pra fechar order-logic.
- Ao retomar: `code 7 find coinspot` + `code 8 coinspot` (o brain já tem tudo que morreu — não re-atacar B/C1/C2/C5/D/quote/C6-limit/G).

### Próximo passo
- **Precisa key full+withdraw** (valor menor agora que C1 morreu, mas ainda vale): **F** (`senddetails`→`send` step-skip / 2FA real na API), **C3/P7** (saque sem OOB = weakness de design), **E/G** (`rate`/`threshold` do cliente no `/my/buy/now`, `/my/buy/edit` localiza por `rate` ou `id`), **A** re-enquadrado (normalização numérica canonical-vs-handler num campo de dinheiro — `{"amount":1e20}` / precisão float — hipótese nova, não testada).
- **Precisa 2ª conta:** K–N (IDOR cross-account real — `buycomplete/{ObjectId}`, `orders/eofy/{ano}`, `deposit/pending/{uuid}`).
- APK: Tiago baixa o `.apk` OU aprova instalar JRE + jadx aqui.
- **Pendente Tiago:** conferir no corpo da policy viva se exige header/tag de identificação (`X-Bug-Bounty`).

---

## Sessão 2026-09-02 — `protocolo ladrão de bancos`

### O que rodou
- Alvo escolhido no `protocolo ladrão de bancos` (portfólio #1). `recon.py --all --rate 5` contra `coinspot.com.au` + `cors_headers_scan` + OSINT (doc da API v2, gau/wayback, JS do site, `.well-known/*`).

### Resultado
- **Superfície minúscula:** 10 subs → 5 resolvem → **2 vivos** (`coinspot.com.au`, `www`). Confirma "closed scope, superfície pequena".
- **gau: 119k URLs** = o corpus principal (katana deu timeout, nuclei 0, sem gf → garimpo manual).
- CORS/headers limpos (nada perseguível).
- **API v2** documentada: 3 superfícies (`/pubapi/v2` GET público · `/api/v2` POST full · `/api/v2/ro` POST read-only), auth **HMAC-SHA512** (`key`+`sign`) + `nonce` monotônico, rate 1000/min. Catálogo de endpoints na ficha.
- **Web `/my/*`** (de gau): `buycomplete/{ObjectId}`, `deposit/pending/{paypal,card}/{uuid}`, `orders/eofy/{ano}`, `my/api` (API keys), `my/bank`, `messagecenter/*`, cadeia `/accountrecovery/{enablewithdrawals,geolockdisable,unlockaccount,2fa}`.
- **Mobile:** Android `com.coinspot.app`, iOS `5Q5H52GDRT.com.coinspot.app` (provável 2 dos 4 assets). Universal links revelam `/withdrawalfiat/confirmed?processed=true`.

### 10 leads mapeados (ficha)
Quentes: #1 IDOR order edit/cancel · #2 quote→execute tampering · #3 race no nonce · #4 withdrawal multi-passo · #5 IDOR buycomplete/deposit-pending · #7 authz divergente entre as 3 APIs. Já dá sem conta: #9 ler `brhash.min.js` (hash de senha client-side?), #10 baixar APK, GitHub code search.

### Bloqueio
Leads quentes precisam de **2 contas + API keys**. Handoff: criar as contas, gerar key read-only em `/my/api`, passar chaves.

## Sessão 2026-09-02 (cont.) — camada UNAUTH, `seguir o rastro` + `formação de lança` + `armar a arapuca`

### Comandos novos criados (CODES.md)
`protocolo One Piece` (caça com 1 conta) · `seguir o rastro` (ramificar de um sinal) · `protocolo armar a arapuca` (inventário de peças → chain).

### Pesquisa externa
- Hack nov/2023: $2.4M hot wallet, **vazamento de chave privada**, "sem detecção em tempo real". Infra, não app.
- **Contradição:** Termos = "2FA p/ todo saque"; doc da API = **zero param de 2FA**, `emailconfirm` default NO.
- **V1 API viva** (`/api`, `/pubapi`, `/api/ro`); `/api/my/coin/deposit` v1 = DEPRECATED mas roteável.

### Caça unauth — `brhash.min.js` NÃO é hash de senha
É lib de **fingerprint de browser** (canvas/fontes/murmurhash3). Lead antigo #9 morto; virou P12 (fingerprint de risco client-controlado).

### `seguir o rastro` — árvore do sinal "erro de nonce antes do erro de key"
- **P1 [forte]:** parser de formato do `nonce` = `parseInt(x,10)` + falsy-guard (fingerprint fechado: `1e30`/`"1e9"`/`-1`/`[1]` passam; `0`/`-0`/`".5"`/`"Infinity"` falham).
- **P2 [forte]:** JSON body = **LAST-KEY-WINS** em chave duplicada (confirmado unauth).
- **P3:** body só parseado com CT `application/json` (match frouxo) ou `x-www-form-urlencoded` — 2 encodings.
- **P4:** método não enforçado na auth; `GET` parseia body JSON; CF só barra PUT/PATCH.
- **P5:** ordem da pipeline mapeada (oráculo de erro).
- **FP:** delay de auth-fail (jitter); "traversal" no pubapi (curl normaliza `../`).

### `armar a arapuca` — `arapuca.md` criado, 12 peças
Chain candidata ⭐ **C1 = P2+P3+P5 → dup-key smuggling no `/my/coin/withdraw/send`** (mandar `amount`/`address` duplicados; se o `sign` cobre bytes crus e a validação lê a 1ª mas o handler a última → saque de valor/destino não-assinado = **Critical**). Elo faltando: 1 API key full+withdraw + saber se o `sign` é canônico.
Outras: C2 (replay nonce cross-versão v1/v2), C3 (key vazada = drain sem OOB).

### Iteração 4 — esgotando o nível 0 (sem conta)
- **GitHub code search:** bloqueado (exige login; grep.app 429). Mas os **wrappers de referência** deram o `sign` = HMAC dos **bytes crus** de `json.dumps` compacto (P13) e `nonce = int(time()*1e6)` (P14) → **C1 fica concreto**.
- **`pages_main.js` → `/ua` e `/ua/event`** (P17): beacon unauth, **sem CSRF, sem throttle**; `fp: fp.get()` enviado sem auth (P12 concreto); `userAgentOverride` = UA que o servidor grava; `path`/`referrer`/`label` sem escape → **blind XSS em dashboard interno** (precisa callback OOB).
- **V1 API doc completa** (P19): menos guardas, path-suffix + assinatura idênticos à V2 → reforça **C2**.
- **Fechados:** `csutm` (sem reflexão server-side) · cache poisoning (nada dinâmico cacheável) · subdomínios (0/~90) · `/withdrawalfiat/confirmed` (cosmética — `?token/id/hash` ignorados) · ALTCHA `?maxnumber=` (server-fixed) · rota que pula middleware (nenhuma).
- **Fora de escopo:** `coinspot.com`/`.co`/`.io` (redirects/parking — H1 só `coinspot.com.au`).

### Bloqueio
**Nível 0 ESGOTADO.** Nível 1 inteiro precisa de **API key**. Restos sem conta: (a) blind XSS `/ua` — precisa dominio de callback (XSS Hunter) teu; (b) APK — baixar `com.coinspot.app` ou instalar `jadx`/`apktool`.

### Encerramento (Selo de saída — 2026-09-02)
- **`modo hunter` + `formação de lança` + `seguir o rastro` + `armar a arapuca` DESLIGADOS.** Pivot pro `protocolo ladrão de bancos` na NBA.
- **Onde parou:** nível 0 esgotado; 19 peças na `arapuca.md`; chain ⭐ **C1** (dup-key smuggling no saque, shape Critical) montada e à espera de 1 API key full+withdraw.
- **Próximo passo:** quando o Tiago tiver conta → `protocolo One Piece` + `formação de lança`, ordem **B → D → C1**. Sem conta: blind XSS `/ua` (precisa OOB) e APK.
- **Aprendizado persistido:** `memory/arsenal.md` ganhou *falsy-guard anti-replay*, *oráculo de pipeline por diferencial de erro*, *beacon de analytics como vetor*.
- **Git:** mudanças não commitadas (Tiago não pediu commit).

## Sessão 2026-09-08 — `protocolo ladrão de bancos` (retomada, sem toque)

### O que rodou
- Retomada do alvo pelo `ladrão de bancos`. `code 7 find` (ficha do board) + `code 8` (brain brief: brain persistente local estava vazio — "fresh"; a fonte real do estado é esta ficha).
- **Passo 3 — constraints reconferidas:** closed scope 4 assets (`coinspot.com.au`, `www`, Android `com.coinspot.app`, iOS), sem wildcard; ~5 req/s web / 1000 req/min API; proibido atacar contas de clientes e brutar `/accountrecovery/*`; automação/IA permitida dentro do rate; Gold Standard Safe Harbor.
- **Gap corrigido:** `scope.md` era o template genérico (`*.exemplo.com`) — o `scope_hook` não detecta template (memória `scope-md-placeholder-gap`). Preenchido com o escopo real da ficha; verificado que o hook agora bloqueia `coinspot.com`/`.co`/`.io` e libera `*.coinspot.com.au`. **Pendente:** reconferir contra a policy viva (privada) antes de subir pra Nível 1.
- Toolchain local ausente (`subfinder`/`httpx`/`katana`/`jadx`/`gh`) — `setup.sh` não rodou neste PC.

### Resultado
- **Nível 0 (passivo) confirmado ESGOTADO** (2026-09-02). Nível 2 (médio) e 3 (completo) já rodados; nada novo perseguível sem conta.
- Nenhum toque novo nesta sessão. Brain: `record coinspot recon "ladrão de bancos — retomada"`.

### Próximo passo
- **Gargalo = handoff humano**, não recon. Quando o Tiago criar as contas + API key → `protocolo One Piece` + `formação de lança`, ordem **B → D → C1**.
- Opção no-touch pendente (se quiser antes do handoff): refresh passivo delta via WebFetch (crt.sh estava 502; Wayback CDX por endpoints novos desde 02/09; urlscan; diff do doc da API v2).

## Próximos passos
1. **Handoff:** 1–2 contas em `www.coinspot.com.au` (1 com alias `<h1username>@wearehackerone.com`); API key em `/my/api` — **1 read-only** primeiro (leads B, D), depois **1 full+withdraw** (A/C1, C, F). Passar `key`+`secret` (via `code -1 coinspot`).
2. Ligar `protocolo One Piece` + `formação de lança`; ordem: **B → D → A (C1)** → E/F/G.
3. Sem conta: GitHub/GitLab code search `coinspot` (keys vazadas — chain C3); baixar APK `com.coinspot.app`.

## Arquivos
- `README.md` · `arapuca.md` (peças + chains) · `recon/` · este log.

## Arquivos
- `README.md` — ficha (10 leads, catálogo da API, status).
- `recon/` — `subdomains.txt`, `live-hosts.txt`, `urls.txt` (119k), `recon.json`, `cors.json`, `webjs/`, logs.
- `a-cacada-ate-aqui.md` — este log.
