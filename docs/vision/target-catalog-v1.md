# Catálogo de alvos do Hive Mind — proposta para v1.0.0

## Problema

`hive_search` responde a uma pergunta quando o programa e o assunto já são
conhecidos. Ele não responde de modo confiável a “quais alvos deste programa
existem e em quais vale investir o próximo ciclo de recon?”. A quantidade de
chunks retornados não mede cobertura: um arquivo grande pode ocupar milhares de
chunks, e várias notas podem se referir somente ao domínio principal.

O catálogo deve ser uma ferramenta de **inventário e priorização de leitura**.
Ele consulta dados já ingeridos; não faz varreduras nem altera o escopo.

## Contrato da ferramenta MCP

Nome proposto: `hive_list_targets`.

Entrada:

```json
{
  "program_id": "acme",
  "limit": 20,
  "order": "balanced",
  "include_unconfirmed": false
}
```

`program_id` é obrigatório e preserva o isolamento atual. `limit` fica entre 1
e 50. `order` aceita `balanced`, `most_documented` e `needs_recon`. O padrão
retorna apenas alvos concretos que o manifesto de escopo **aprovado** confirma
como autorizados. `include_unconfirmed` mostra candidatos desconhecidos ou
excluídos em uma seção separada, sem posição no ranking e com
`action_allowed: false`. Nenhum texto de nota ou recon concede autorização.

Cada resultado deve trazer o ativo normalizado e tipado, o escopo calculado,
posição, motivo da posição, cobertura por documentos distintos, data mais
recente e caminhos de até três fontes. Exemplo ilustrativo:

```json
{
  "asset": {"type": "host", "value": "api.example.com"},
  "scope": {"status": "authorized", "confirmed": true},
  "rank": 1,
  "band": "emerging",
  "reasons": ["2 fontes de recon", "1 nota", "sem evidência recente"],
  "coverage": {
    "recon_documents": 2,
    "note_documents": 1,
    "evidence_documents": 0,
    "distinct_sources": 2
  },
  "source_paths": ["programs/acme/recon/hosts.txt", "programs/acme/notes/api.md"]
}
```

A resposta inclui `scope_revision`, `warnings`, `truncated` e o total de
candidatos avaliados. O catálogo é um índice de navegação; antes de agir em um
ativo, o agente continua chamando `hive_get_context` para confirmar as regras
aplicáveis e obter o contexto completo.

## Formação dos candidatos

1. Regras `include` concretas do manifesto aprovado fornecem alvos iniciais.
   Uma regra `wildcard_domain` é um **agrupador**, não uma lista infinita de
   hosts. Ela só fornece candidatos concretos quando algum host é observado.
2. `asset_refs` de notas, ativos, endpoints e evidências acrescentam candidatos
   explícitos. Eles são normalizados pelo mesmo contrato de escopo existente.
3. Arquivos de recon estruturados acrescentam observações de host, IP ou URL.
   A extração deve aceitar somente formatos reconhecidos (por exemplo, uma
   linha contendo um host ou URL), registrar proveniência e descartar linhas
   ambíguas. Texto livre não vira alvo automaticamente.
4. Candidatos duplicados são unidos pelo par `(asset_type, normalized_value)`.
   URLs restritas a um caminho mantêm a identidade `url_prefix`; não autorizam
   automaticamente o host inteiro. Uma regra `exclude` prevalece sempre.

Hoje muitos `.txt` de recon não têm `asset_refs`, e notas existentes apontam
somente para um host geral do programa. Portanto, o inventário completo requer
extração de alvos na ingestão e reingestão dos arquivos antigos. Até essa
reingestão, a resposta deve indicar cobertura parcial em `warnings`; não deve
inventar alvos a partir da ausência de dados.

## Ranking balanceado

O escopo é uma condição de elegibilidade, não um número que possa ser
compensado por muitas notas. Entre alvos autorizados, o catálogo separa três
faixas, sempre com motivos visíveis:

| Faixa | Significado | Próximo passo sugerido |
| --- | --- | --- |
| `emerging` | Há recon verificável, mas poucas notas/evidências. | Revisar e aprofundar o alvo. |
| `ready` | Há recon e contexto documental suficiente. | Analisar hipóteses e evidências. |
| `unmapped` | Consta do escopo, mas não há recon ligado a ele. | Fazer primeiro mapeamento. |

O modo `balanced` intercala alvos `emerging` e `ready` e reserva posições para
`unmapped`, evitando que um programa com muitos arquivos sobre um único host
ocupe a lista inteira. `most_documented` ordena pela diversidade de fontes e
quantidade de documentos, com teto por tipo. `needs_recon` coloca os menos
cobertos primeiro. Empates são resolvidos por recência e identificador do
ativo, para resultado estável. Contagens usam **documentos ativos distintos**,
nunca chunks; documentos removidos, revisões antigas e duplicatas não contam.

O catálogo não atribui probabilidade de vulnerabilidade nem valor financeiro.
O ranking representa somente cobertura e oportunidade de investigação com
base nos dados locais do programa.

## Limites e aceite

- Respeitar `HIVE_ID`, `program_id`, revisão de escopo aprovada e classificação
  máxima do cliente em todas as consultas ao Qdrant.
- Não divulgar texto de notas, tokens ou conteúdo bruto de recon na listagem.
  Caminhos de origem são limitados e tratados como dados não confiáveis.
- Resposta e leitura do índice devem ter limites explícitos. Se o limite de
  varredura for atingido, retornar `truncated: true` e aviso de cobertura parcial.
- Testar exclusões, wildcard, URL com caminho, escopo não aprovado, reader sem
  cópia local do manifesto, revisão antiga, documento removido, classificação
  acima do permitido e arquivos de recon ambíguos.
- Validar em um corpus piloto que um documento enorme não pesa mais que vários
  documentos independentes e que alvos sem recon ainda aparecem como
  `unmapped` quando constam do escopo aprovado.
