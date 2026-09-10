# Segurança 04 — Gestão de segredos

## Objetivo

Impedir que credenciais sejam versionadas, exibidas ou mantidas além do necessário.

## Requisitos

- Segredos entram por variável de ambiente injetada ou gerenciador de segredos; arquivos locais de segredo ficam ignorados pelo Git e com permissão restrita.
- `.env.hive.example` contém somente placeholders reconhecíveis.
- Chaves nunca aparecem em argumentos de processo, logs, erros, métricas, testes, fixtures ou respostas MCP.
- Funções de configuração separam valores sensíveis para que impressão acidental seja difícil.
- Credenciais Qdrant são próprias por ambiente, dispositivo e papel, limitadas às collections do Hive. A chave administrativa fica fora dos processos normais. Existem procedimentos de geração, distribuição, rotação e revogação.
- Segredos de teste são sintéticos e não reutilizados em ambiente real.

## Detecção

CI executa scanner de segredos no histórico/alteração e bloqueia achados confirmados. Um vazamento suspeito é tratado como comprometimento: revogar primeiro, investigar depois.

## Aceite e testes

- Testes injetam valores sentinela e confirmam ausência em toda saída capturada.
- Arquivos locais esperados estão no `.gitignore`.
- O runbook de rotação e revogação individual é executado antes da v0.1 e registra apenas identificador/data, nunca a chave.
