# Acesso privado ao Qdrant

O processo Hive nunca recebe a chave administrativa. Na implantação self-hosted, a chave administrativa fica somente no secret store do serviço Qdrant e habilita `jwt_rbac`. Fora de loopback, habilite TLS diretamente no Qdrant ou em um proxy privado autenticado e use apenas o endereço da VPN/rede privada.

Crie tokens JWT HS256 fora do processo Hive, com expiração e `sub` identificando o dispositivo. Cada writer recebe `rw` e cada reader recebe `r`, sempre um token por dispositivo, exclusivamente para as duas collections configuradas:

```json
{
  "sub": "workstation-a",
  "exp": 1798761600,
  "access": [
    {"collection": "hive_mind_v01", "access": "rw"},
    {"collection": "hive_mind_v01__control", "access": "rw"}
  ]
}
```

Para um reader, substitua os dois valores `rw` por `r` e use um `sub` próprio. Injete somente o token resultante como `QDRANT_API_KEY`. Nunca armazene o segredo de assinatura, token ou conteúdo do Hive neste repositório.

## Provisionamento e validação

Use a API REST privada do cliente administrativo (normalmente porta `6333`, não a porta gRPC usada pelo Hive). Descubra a dimensão do modelo local pela resposta de `POST /api/embed` do Ollama e conte os elementos de `embeddings[0]`; não presuma a dimensão de outro modelo.

Crie a collection de dados com distância cosseno e vetor esparso nomeado `sparse`. Substitua `768` pela dimensão conferida:

```json
{"vectors":{"size":768,"distance":"Cosine"},"sparse_vectors":{"sparse":{}}}
```

Crie `<HIVE_COLLECTION>__control` sem vetores:

```json
{"vectors":{}}
```

Essas são operações administrativas de criação. Confira que o destino está vazio e pertence ao Hive correto. Não exclua uma collection existente para contornar incompatibilidade. O primeiro writer cria índices de payload e grava o manifesto no primeiro `ingest`; cada writer grava seu próprio registro (`writer_registration`) no primeiro `ingest`. Tokens normais permanecem restritos às duas collections.

1. Um operador provisiona as duas collections e índices em uma janela administrativa quando ainda não existem. Qualquer writer pode reconciliar infraestrutura existente, mas seu token normal permanece limitado às duas collections.
2. Inicie cada processo com seu token individual. A validação lê ambas as collections. Para readers, ela também exige que uma tentativa de escrita seja negada explicitamente; para writers, confirma escrita e remoção de um registro de prova sem conteúdo e a presença do próprio registro com o `HIVE_WRITER_APPROVAL_ID` configurado.
3. Confirme que o Compose de referência só responde em loopback. Para acesso remoto, mantenha o Qdrant em loopback e publique apenas um proxy TLS autenticado na interface VPN; não vincule as portas do Qdrant a `0.0.0.0`.
4. Em acesso remoto, execute `validate` com `QDRANT_URL=https://...`; HTTP remoto é rejeitado antes da conexão.

## Rotação e revogação

Gere um token novo com validade sobreposta curta, atualize apenas o dispositivo correspondente, execute `validate` e então expire/revogue o token antigo. Emitir outro JWT não revoga automaticamente o anterior: configure um mecanismo efetivo de revogação e comprove o bloqueio, inclusive pela identidade de rede. Para remover um dispositivo, revogue primeiro sua identidade de rede e seu token; depois verifique os logs desde o último acesso conhecido. Para admitir um writer, emita um token `rw` próprio e um `HIVE_WRITER_APPROVAL_ID` próprio; o registro é criado pelo próprio writer no primeiro `ingest`. Para remover um writer, revogue seu token e identidade de rede, registre `audit record credential_revocation` e decida o destino dos documentos que ele possui (permanecem ativos até tombstone).

Registre somente dispositivo, operador, data, resultado e identificador do change. Nunca registre o token. Uma implantação sem JWT granular é uma exceção de segurança: requer instância exclusiva, decisão com prazo/risco e bloqueia o gate normal de release enquanto não for aprovada.

## Admissão de dispositivo

1. Registre o `HIVE_DEVICE_ID`, papel, operador aprovador e change, sem credenciais.
2. Autorize a identidade do dispositivo na VPN/firewall e emita um JWT exclusivo, de vida limitada, com acesso somente às duas collections.
3. Instale o token pelo secret manager do dispositivo e execute `validate`.
4. Para readers, confirme no relatório que leitura funciona e a prova de escrita é negada; para cada writer, confirme escrita e remoção da prova e o registro próprio.

## Revogação de dispositivo

1. Remova primeiro a identidade da VPN/firewall e revogue ou expire o JWT individual.
2. Confirme que `validate` falha com código `12` no dispositivo removido.
3. Examine os logs sanitizados desde o último acesso conhecido e rotacione outros segredos apenas se houve compartilhamento ou exposição.
4. Registre dispositivo, data, operador, change e resultado. Não copie mensagens que contenham tokens.
