# Segurança 06 — Ingestão e recuperação seguras

## Objetivo

Tratar arquivos e conteúdo recuperado como dados não confiáveis, impedindo leitura fora do diretório e confusão entre evidência e instrução.

## Ingestão

- Resolver e validar caminho real dentro de `HIVE_DATA_DIR`; recusar escapes, symlinks externos, dispositivos, sockets e arquivos especiais.
- Limitar tamanho do arquivo, quantidade/tamanho dos chunks, profundidade e ritmo de processamento.
- Aceitar somente formatos declarados; validar JSON e recusar conteúdo binário.
- Não executar macros, scripts, links, comandos ou conteúdo incorporado.
- Usar escrita idempotente e preservar o último estado íntegro se uma reindexação falhar.

## Recuperação MCP

- Marcar trechos como conteúdo não confiável e separá-los das instruções da ferramenta.
- Nunca tratar texto recuperado como autorização de escopo sem metadado validado.
- Limitar resultados e tamanho total; escapar a formatação necessária para impedir quebra da estrutura da resposta.
- Erros não retornam paths absolutos, stack traces, consultas internas ou credenciais.

## Aceite e testes

- Fixtures cobrem path traversal, symlink externo, arquivo especial, JSON inválido, documento enorme e conteúdo com instruções maliciosas.
- Falha parcial não apaga dados válidos anteriores nem grava conjunto incompleto como concluído.
- Respostas mantêm origem e limites mesmo com Markdown adversarial.
