# 002 — Segurança é o requisito prioritário

- Estado: Aceita
- Data: 2026-09-09

## Contexto

O Hive Mind armazena e transporta informações de recon, escopo e evidências. A confidencialidade desses dados, a integridade dos resultados e o controle de quem pode acessá-los são requisitos centrais do produto.

## Decisão

Todo componente deve ser desenhado com menor privilégio, autenticação explícita e criptografia em trânsito. O MCP permanece local via `stdio`; somente serviços necessários podem se comunicar pela rede privada. Qdrant não pode ser exposto publicamente e deve exigir autenticação. TLS é obrigatório sempre que o tráfego sair do host, salvo uma exceção documentada para uma rede privada controlada e aprovada.

Segredos nunca são versionados, incluídos em exemplos, logs, mensagens MCP ou saídas de diagnóstico. O acesso é limitado a máquinas, usuários e coleções autorizados. Falhas de validação de segurança bloqueiam ingestão e consulta quando poderiam causar acesso indevido ou tráfego inseguro.

## Consequências

- Conveniência de configuração não pode reduzir autenticação, isolamento ou proteção de transporte.
- Cada spec deve definir ameaças, controles e testes de segurança pertinentes.
- Logs precisam ser estruturados para auditoria sem registrar conteúdo sensível desnecessário.
- Qualquer integração futura com usuários, times ou serviços externos deve definir autorização antes de expor dados.
