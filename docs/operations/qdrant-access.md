# Acesso privado ao Qdrant

O processo Hive nunca recebe a chave administrativa. Na implantação self-hosted, a chave administrativa fica somente no secret store do serviço Qdrant e habilita `jwt_rbac`. Fora de loopback, habilite TLS diretamente no Qdrant ou em um proxy privado autenticado e use apenas o endereço da VPN/rede privada.

Crie tokens JWT HS256 fora do processo Hive, com expiração e `sub` identificando o dispositivo. O writer recebe `rw` e cada reader recebe `r`, exclusivamente para as duas collections configuradas:

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

1. Um operador provisiona as duas collections e índices em uma janela administrativa quando ainda não existem. O writer pode reconciliar infraestrutura existente, mas seu token normal permanece limitado às duas collections.
2. Inicie cada processo com seu token individual. A validação lê ambas as collections. Para readers, ela também exige que uma tentativa de escrita seja negada explicitamente; para o writer, confirma escrita e remoção de um registro de prova sem conteúdo.
3. Confirme que o Qdrant só responde em loopback ou no IP privado indicado por `HIVE_QDRANT_BIND_IP`. Não use `0.0.0.0`.
4. Em acesso remoto, execute `validate` com `QDRANT_URL=https://...`; HTTP remoto é rejeitado antes da conexão.

## Rotação e revogação

Gere um token novo com validade sobreposta curta, atualize apenas o dispositivo correspondente, execute `validate` e então expire/revogue o token antigo. Para remover um dispositivo, revogue primeiro sua identidade de rede e seu token; depois verifique os logs desde o último acesso conhecido. Para promover writer, revogue o writer anterior antes de emitir o novo token `rw`.

Registre somente dispositivo, operador, data, resultado e identificador do change. Nunca registre o token. Uma implantação sem JWT granular é uma exceção de segurança: requer instância exclusiva, decisão com prazo/risco e bloqueia o gate normal de release enquanto não for aprovada.
