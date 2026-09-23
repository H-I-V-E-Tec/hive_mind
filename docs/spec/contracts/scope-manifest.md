# Contrato do manifesto de escopo v1

O arquivo autoritativo de cada programa é `programs/<program_id>/scope.json` e deve validar contra [`scope-manifest.schema.json`](scope-manifest.schema.json). O `program_id` do conteúdo deve ser idêntico ao segmento do path.

Para registrar a origem do manifesto, use `platform` (`h1`, `bugcrowd` etc.) e
`target_name: "@program"` em conjunto: o escopo cobre o programa inteiro,
não um único projeto. `classification` pode marcar o documento como `internal`
ou `restricted` para busca. Manifestos históricos sem esses campos continuam válidos;
incluí-los muda os bytes e o SHA-256 aprovado, exigindo nova aprovação.

## Normalização

- `host`: hostname IDNA em A-label, minúsculo, sem ponto final; correspondência exata.
- `wildcard_domain`: começa obrigatoriamente por `*.` e usa a mesma normalização de host. Corresponde a subdomínios, mas não ao domínio raiz.
- `ip`: endereço IPv4 ou IPv6 canônico; correspondência exata.
- `cidr`: rede IPv4 ou IPv6 canônica; corresponde quando contém o IP consultado.
- `url_prefix`: URL absoluta HTTP/HTTPS, hostname normalizado, fragmento proibido e porta padrão removida. Corresponde por origem idêntica e segmentos de path, nunca por prefixo textual parcial de hostname.

Valores que não possam ser normalizados invalidam o manifesto inteiro. Duplicatas idênticas são erro de validação, não são removidas silenciosamente.

## Decisão de escopo

Para um ativo normalizado:

1. Se o manifesto estiver ausente, inválido, alterado depois da aprovação ou não permitir representar o ativo, o resultado é `unknown`.
2. Se qualquer regra `exclude` corresponder, o resultado é `out_of_scope`.
3. Caso contrário, se alguma regra `include` corresponder, o resultado é `authorized`.
4. Sem correspondência, o resultado é `unknown`.

A aprovação grava na collection de controle `program_id`, SHA-256 dos bytes exatos do manifesto, schema version, `HIVE_DEVICE_ID` aprovador e timestamp UTC. Conteúdo do manifesto não é copiado para logs.

## Exemplo

```json
{
  "schema_version": 1,
  "program_id": "acme-bugbounty",
  "platform": "h1",
  "target_name": "@program",
  "classification": "internal",
  "source": "program policy portal",
  "collected_at": "2026-09-10T12:00:00Z",
  "rules": [
    {"action": "include", "asset_type": "wildcard_domain", "value": "*.example.com"},
    {"action": "exclude", "asset_type": "host", "value": "billing.example.com", "reason": "third-party"}
  ]
}
```

Nesse exemplo, `api.example.com` é autorizado, `billing.example.com` está fora de escopo e `example.com` permanece desconhecido.
