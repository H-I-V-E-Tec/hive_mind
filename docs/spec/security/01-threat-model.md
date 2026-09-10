# Segurança 01 — Modelo de ameaças

## Objetivo

Manter um modelo de ameaças versionado que oriente arquitetura, implementação e testes antes de expor dados reais.

## Ativos protegidos

- documentos de recon, evidências, regras e escopo;
- vetores e payloads, pois também podem revelar informações sensíveis;
- credenciais do Qdrant, VPN e serviços locais;
- integridade dos resultados e vínculo com sua fonte;
- identidade dos dispositivos autorizados e registros de auditoria.

## Limites de confiança

```text
Agente ─stdio─ MCP reader ─TLS/VPN─ Qdrant privado
                  │                       ▲
             Ollama local                 │
                                          │
Operador ─CLI─ MCP writer ────────────────┘
                  │
             Ollama local
                  │
        hive-data canônico/sincronizado
```

`stdio` não torna automaticamente o cliente confiável. Arquivos sincronizados e conteúdo indexado são entradas não confiáveis. Somente um manifesto de escopo validado e com hash aprovado fora do fluxo MCP pode conceder autorização. Rede privada reduz exposição, mas não substitui autenticação nem criptografia.

## Ameaças mínimas

- acesso por dispositivo, processo ou pessoa não autorizada;
- captura ou alteração do tráfego;
- vazamento por logs, erros, backups, Git ou respostas MCP;
- mistura de dados entre `hive_id` ou programas;
- documento malicioso tentando instruir o agente;
- path traversal, symlink e leitura fora de `HIVE_DATA_DIR`;
- adulteração, replay ou resultados obsoletos;
- disputa entre writers, atraso de sincronização ou publicação parcial;
- dependência comprometida e binário adulterado;
- perda de dados ou indisponibilidade do Qdrant/Ollama.

## Aceite

- Cada ameaça possui ao menos um controle e teste referenciados em uma matriz de rastreabilidade mantida na validação de segurança.
- Mudanças de arquitetura atualizam este documento antes da implementação.
- A revisão de release registra ameaças novas, mitigadas e aceitas.
