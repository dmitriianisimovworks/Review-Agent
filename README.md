
# Technical Specification Review Agent

Backend-сервис для автоматического review технических спецификаций с интеграцией в Google Docs.

Агент:
- читает документ из Google Docs или из прямого API-вызова;
- разбивает документ на структурные секции и чанки;
- прогоняет их через набор reviewer-ролей;
- публикует findings и summary обратно в Google Docs;
- поддерживает повторные прогоны, review memory, duplicate suppression и section-based rerun.

## Что умеет

- `full review` всего документа
- `incremental review` с учетом предыдущего контекста
- review конкретного раздела по команде
- history-aware review через `review_key`
- suppression повторов между итерациями
- vector memory: indexing findings/summaries/sections в `Qdrant`
- retrieval похожих сигналов обратно в review context
- cleanup комментариев агента
- автообнаружение shared Google Docs
- регистрация документа по `google_doc_url`
- tracked documents registry для shared docs
- MCP context server для внешних coding agents

## Роли анализа

По умолчанию агент использует несколько reviewer-ролей с разным фокусом:

- `Tech Lead`
- `Solution Architect`
- `Senior Backend Engineer`
- `Senior Frontend Engineer`
- `Mobile Lead`
- `DevOps Reviewer`
- `QA Reviewer`
- `Security Lead`

После ответа модели backend дополнительно нормализует findings:
- убирает смысловые дубли;
- назначает более подходящего owner для типовых тем;
- схлопывает близкие findings в общие темы для summary.

## Основные команды в Google Docs

```text
@review-agent
@review-agent full
@review-agent incremental
@review-agent incremental section: 4.5.7
@review-agent cleanup
```

## Режимы работы

- `full review`
  Полный проход по документу с разбором всех доступных чанков.

- `incremental review`
  Повторный прогон с учетом предыдущей истории review и suppression уже известных проблем.

- `incremental section`
  Повторный прогон только по конкретному разделу, например `4.5.7`.

- `cleanup`
  Удаление comments, созданных самим агентом, без запуска нового анализа.

## Как работает

1. Документ попадает в систему через folder polling, auto-discovery shared docs или прямой API-вызов.
2. Google Docs reader вытягивает текст, секции и блоки документа.
3. Документ режется на чанки.
4. Каждый chunk анализируется reviewer-ролями через LLM.
5. Backend делает shaping результата:
   dedup, role ownership, theme clustering, summary aggregation.
6. Findings и artifacts сохраняются в PostgreSQL.
7. Runtime state и async jobs проходят через Redis.
8. Semantic artifacts индексируются в Qdrant и могут возвращаться в prompt context.
9. Итоговые comments публикуются обратно в Google Docs.

## Memory и повторные прогоны

Система использует два слоя памяти:

- `review memory`
  Хранит summaries, findings и artifacts прошлых прогонов в рамках одного `review_key`.

- `vector memory`
  Индексирует summaries, findings и document sections в `Qdrant`, чтобы возвращать похожие сигналы в новый review context.

Это позволяет:
- не повторять одинаковые замечания между итерациями;
- усиливать incremental review за счет предыдущей истории;
- использовать накопленный контекст не только в пределах одного запуска.

## Shared Google Docs

Агент умеет работать не только с inbox-папкой, но и с произвольными shared Google Docs.

Поддерживаются два сценария:

- ручная регистрация документа по `google_doc_url` через API;
- автообнаружение документов, к которым service account уже получил доступ.

После регистрации или автообнаружения команды в comments начинают обрабатываться так же, как и для inbox-документов.

## Стек

- `Go`
- `Chi`
- `PostgreSQL`
- `Redis`
- `Qdrant`
- `Google Docs API`
- `Google Drive API`
- `Docker Compose`
- `Nginx`
- `TypeScript` MCP server

## HTTP API

Основные endpoints:

```text
GET  /health
POST /api/v1/analyses
GET  /api/v1/analyses/{analysisID}
POST /api/v1/analyses/{analysisID}/publish-comments
POST /api/v1/google/docs/register
GET  /api/v1/google/oauth/start
GET  /oauth/google/callback
```

Пример запуска анализа по Google Doc:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/analyses \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Spec",
    "source": "google_docs",
    "mode": "full_review",
    "google_doc_url": "https://docs.google.com/document/d/DOC_ID/edit"
  }'
```

## Запуск локально

Требуется настроенный `.env` и service account для Google APIs.

```bash
docker compose up -d --build
```

Проверка:

```bash
curl http://127.0.0.1:8080/health
```

Если документ уже доступен service account, можно зарегистрировать его вручную:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/google/docs/register \
  -H 'Content-Type: application/json' \
  -d '{
    "google_doc_url": "https://docs.google.com/document/d/DOC_ID/edit"
  }'
```

## Структура проекта

- `cmd/api` — entrypoint приложения
- `internal/app` — wiring зависимостей
- `internal/api/http` — HTTP handlers и router
- `internal/service` — core business logic
- `internal/integration/google` — Google Docs / Drive integration
- `internal/integration/llm` — LLM client
- `internal/integration/vector` — embeddings и Qdrant
- `internal/comment` — formatter comments и summary
- `internal/reviewshape` — dedup и taxonomy логика
- `internal/platform/postgres` — migrations
- `mcp/project-context-server` — локальный MCP server

## Статус

Проект покрывает полный end-to-end flow:
- document intake
- async analysis
- publish-back в Google Docs
- tracked docs
- command-based rerun
- memory и vector memory
- taxonomy-driven shaping результата
- cleanup commands
- auto-discovery shared docs
=======
=======
info in mcp
