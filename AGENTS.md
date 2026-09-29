# Trabalho neste repositório

Este checkout é usado para desenvolver, testar e publicar o Hive Mind.

- Agentes operacionais usam o cliente instalado em `../hive_instance`, com o launcher e a configuração dessa instância.
- Não configure este checkout como MCP operacional, writer, reader ou fonte de documentos reais. Não recrie `hive-data/` e não carregue credenciais de produção aqui.
- Desenvolvimento e testes podem executar o código com fixtures sintéticas e diretórios temporários isolados, sem acessar o acervo ou as collections de produção.
- Mantenha contratos, schemas, decisões e fixtures sintéticas versionados. Dados de clientes, segredos, logs, backups e artefatos gerados ficam fora do Git e do contexto Docker.
- Os guias de operação ficam em `../guias`; os READMEs e a documentação normativa continuam neste repositório. Templates em `server/skills/` fazem parte do produto e devem continuar disponíveis no binário.
- A nova API, identidade e portal de membros pertencem ao projeto irmão `../hive_center`; mantenha aqui apenas os contratos necessários ao cliente e os links para aquele projeto.
- Antes de retirar arquivos reais de um workspace observado, verifique se existe um writer ativo. Limpeza de checkout não deve publicar exclusões no serviço.
- Consulte `PLANO_MELHORIA_E_LIMPEZA.md` para a divisão atual entre cliente/servidor e a arquitetura proposta. Execute os testes pertinentes às mudanças; não declare aceite de infraestrutura usando apenas mocks.
