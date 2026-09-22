---
program_id: demo
document_type: evidence
claimed_scope_status: unknown
classification: internal
source: synthetic-fixture
collected_at: 2026-09-10T09:00:00Z
tags: [relatorio, cometa]
---
# Relatório consolidado do ensaio Cometa

## Resumo executivo

O ensaio Cometa avaliou o ambiente orion durante duas semanas. Foram identificadas onze fragilidades, três de severidade alta.

## Metodologia

Reconhecimento passivo, enumeração ativa autorizada, análise de autenticação e revisão de configuração TLS, seguindo as regras do programa demo.

## Autenticação e sessão

O endpoint de login /v2/session usa JWT HS256 com segredo curto. O módulo Cadeado limita tentativas, mas o refresh não invalida sessões antigas.

## Gateway e balanceamento

O gateway gateway.example.test com Kestrel 4.2.1 expõe /healthz sem autenticação e revela o build do cluster orion.

## Serviço de mídia

O buscador de imagens em media.example.test permite SSRF para metadados da instância Andorinha e para a rede interna 10.40.0.0/16.

## Faturamento

IDOR em billing.example.test permite ler faturas de terceiros por enumeração de identificadores sequenciais.

## VPN e TLS

O concentrador vpn.example.test aceita TLS 1.0 e cifras 3DES; o certificado da CA Ferrugem expira em novembro.

## Recomendações

Rotacionar o segredo JWT, exigir autenticação em /healthz, filtrar destinos do buscador de imagens, autorizar acesso por conta nas faturas e desativar TLS 1.0.

