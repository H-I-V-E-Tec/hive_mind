# Setup multi-máquina — 4 pessoas, redes diferentes

Como colocar 4 pesquisadores em redes distintas escrevendo/lendo no **mesmo Hive** pela
internet, com segurança. Base: `qdrant-access.md` + `four-person-trial.md`.

## Princípio (o que é compartilhado vs local)

**Só o Qdrant é compartilhado.** Ollama, o binário MCP, `HIVE_DATA_DIR` e `HIVE_AUDIT_DIR`
são **locais em cada máquina**. O contexto do colega chega porque a busca devolve o **texto
do chunk** do payload do Qdrant — ninguém precisa dos arquivos do outro em disco.

```
        ┌──────────── Qdrant privado (1 host sempre ligado) ────────────┐
        │ loopback + TLS na interface da VPN · JWT RBAC (1 token/pessoa) │
        │ collections: hive_mind_v01  +  hive_mind_v01__control          │
        └────▲──────────────▲──────────────▲──────────────▲─────────────┘
   gRPC+TLS  │  token rw por dispositivo · HTTPS obrigatório             │
      ┌──────┴───┐  ┌────────┴─┐  ┌─────────┴┐  ┌─────────┴┐
      │ Máquina A│  │ Máquina B│  │ Máquina C│  │ Máquina D│
      │ MCP+Ollama  │ MCP+Ollama  │ MCP+Ollama  │ MCP+Ollama  ← Ollama SEMPRE local
      │ data/+audit │ data/+audit │ data/+audit │ data/+audit ← por pessoa
      └──────────┘  └──────────┘  └──────────┘  └──────────┘
```

## Passo 1 — Rede privada por cima da internet (Tailscale)

Malha WireGuard: IPs privados estáveis, criptografados, atravessa NAT, sem abrir porta.

```bash
# no host do Qdrant e nas 4 máquinas:
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up            # login uma vez; anote o nome MagicDNS do host (ex.: qdrant-host.<tailnet>.ts.net)
tailscale ip -4              # IP 100.x do host
```
Trave o acesso com uma ACL do Tailscale (só os 5 dispositivos, só a porta do Qdrant).
Alternativas equivalentes: Netbird/Headscale (self-hosted) ou WireGuard puro.

## Passo 2 — Host do Qdrant (loopback + TLS na interface da VPN)

O `docker-compose.yml` do repo já sobe o Qdrant em loopback com `JWT_RBAC=true`. Falta só
o TLS na interface da VPN. **Nunca** vincule 6333/6334 a `0.0.0.0`.

```bash
export HIVE_QDRANT_ADMIN_KEY="$(openssl rand -base64 48)"   # segredo de assinatura dos JWT; NUNCA sai do host
docker compose up -d qdrant                                  # REST 6333 / gRPC 6334 em 127.0.0.1
```

TLS na frente do gRPC (6334) via Caddy, escutando **só no IP Tailscale** (cert do próprio
Tailscale, publicamente confiável):

```bash
tailscale cert qdrant-host.<tailnet>.ts.net    # gera cert+key válidos
```
`Caddyfile`:
```
qdrant-host.<tailnet>.ts.net:6443 {
    tls /var/lib/tailscale/certs/qdrant-host.<tailnet>.ts.net.crt \
        /var/lib/tailscale/certs/qdrant-host.<tailnet>.ts.net.key
    reverse_proxy h2c://127.0.0.1:6334          # gRPC h2c interno → TLS externo
    bind <IP-tailscale-100.x>                    # só na interface da VPN
}
```
> Alternativa sem Caddy: TLS nativo do Qdrant (`QDRANT__SERVICE__ENABLE_TLS`) — mais fiddly.
> `QDRANT_URL` das máquinas apontará pra `https://qdrant-host.<tailnet>.ts.net:6443`.

## Passo 3 — Provisionar (operador, uma vez)

Descubra a **dimensão do modelo** (não presuma 768):
```bash
DIM=$(curl -fsS http://127.0.0.1:11434/api/embed -d '{"model":"nomic-embed-text","input":"x"}' \
      | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["embeddings"][0]))')
echo "dim=$DIM"
```
Crie as 2 collections (REST 6333, via VPN ou no host):
```bash
curl -fsS -X PUT "http://127.0.0.1:6333/collections/hive_mind_v01" \
  -H "api-key: $HIVE_QDRANT_ADMIN_KEY" -H 'content-type: application/json' \
  -d "{\"vectors\":{\"size\":$DIM,\"distance\":\"Cosine\"},\"sparse_vectors\":{\"sparse\":{}}}"
curl -fsS -X PUT "http://127.0.0.1:6333/collections/hive_mind_v01__control" \
  -H "api-key: $HIVE_QDRANT_ADMIN_KEY" -H 'content-type: application/json' \
  -d '{"vectors":{}}'
```
Emita **1 token JWT rw por dispositivo** (HS256, assinado com o admin key). `mint_jwt.py`:
```python
import sys, time, hmac, hashlib, base64, json
def b64(b): return base64.urlsafe_b64encode(b).rstrip(b"=").decode()
secret, sub, days = sys.argv[1], sys.argv[2], int(sys.argv[3] if len(sys.argv)>3 else 30)
hdr = {"alg":"HS256","typ":"JWT"}
pl  = {"sub":sub, "exp":int(time.time())+days*86400,
       "access":[{"collection":"hive_mind_v01","access":"rw"},
                 {"collection":"hive_mind_v01__control","access":"rw"}]}
seg = b64(json.dumps(hdr,separators=(',',':')).encode())+"."+b64(json.dumps(pl,separators=(',',':')).encode())
sig = b64(hmac.new(secret.encode(), seg.encode(), hashlib.sha256).digest())
print(seg+"."+sig)
```
```bash
python3 mint_jwt.py "$HIVE_QDRANT_ADMIN_KEY" alice-laptop 30   # repita p/ bob, carol, dave
# registre cada emissão (sem o token): ./bin/hive-mind audit record credential_rotation <change-id>
```
Para reader (Codex), troque os dois `rw` por `r`.

## Passo 4 — Cada máquina (×4)

`ollama pull nomic-embed-text` (⚠️ **mesmo modelo+digest nas 4**). `.env.hive`:
```bash
HIVE_ID=research-team                 # IGUAL nas 4
HIVE_COLLECTION=hive_mind_v01         # IGUAL nas 4
HIVE_DEVICE_ID=alice-laptop           # ÚNICO por máquina
HIVE_ROLE=writer
HIVE_WRITER_APPROVAL_ID=change-alice-001   # próprio (1 ticket por pessoa; imutável após 1º ingest)
HIVE_DATA_DIR=/caminho/local/hive-data     # próprio
HIVE_AUDIT_DIR=/caminho/local/hive-audit   # próprio, fora do data dir
QDRANT_URL=https://qdrant-host.<tailnet>.ts.net:6443   # HTTPS obrigatório (HTTP remoto é rejeitado)
QDRANT_API_KEY=<token JWT rw desta pessoa>             # via secret manager; NUNCA versionado
# QDRANT_TLS_CA_FILE=/caminho/ca.pem                   # só se cert self-signed; cert Tailscale dispensa
OLLAMA_URL=http://127.0.0.1:11434     # LOCAL
EMBEDDING_MODEL=nomic-embed-text      # IGUAL nas 4
HIVE_MAX_CLASSIFICATION=internal
```
Registrar o writer e validar:
```bash
hive-mind ingest      # 1x com a pasta de notas vazia → grava writer_registration
hive-mind validate    # deve passar com o próprio writer_registration; código 12/13 = auth/TLS
```
Aponte o MCP do Claude Code/Codex pro binário e instale a skill (`install-skill claude`|`codex`).

## Passo 5 — Rodar o ensaio

Siga `four-person-trial.md`: semana 1 (times separados) → handoff dia 8 (só via Hive, sem
conversar) → semana 2 (programas trocados). Meça com `hive-mind audit report --since=<data>`.

## Checklist de armadilhas (as que pegam)

- [ ] **Mesmo modelo de embedding + digest nas 4** — vetores de modelos diferentes são incomparáveis. `validate` compara o fingerprint.
- [ ] **Dimensão da collection = dimensão do modelo** (conferida pelo Ollama, não presumida).
- [ ] **HTTPS remoto** — o binário rejeita HTTP fora de loopback antes de conectar.
- [ ] **Admin key só no host** do Qdrant; máquinas recebem só o JWT `rw` individual.
- [ ] **Ollama nunca exposto** — só o Qdrant é compartilhado.
- [ ] **Relógio sincronizado** (JWT `exp` + timestamps de audit).
- [ ] Nota só aparece pro colega após `ingest_workspace`/watcher de quem escreveu (`get_sync_status`).
- [ ] Colisão de path → 2ª pessoa vê `skipped`; prefixe arquivos com o handle.
- [ ] Revogar dispositivo = tirar da VPN/firewall **primeiro**, depois expirar o JWT (`qdrant-access.md`).
