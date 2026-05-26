# Progress

## Done

- Реализована полная `Google Docs` интеграция: intake документа, авто-подбор review thread, публикация comments обратно в документ.
- Добавлен `incremental review` по командам из comments, включая rerun по секции (`section: 4.2`, `5.1` и т.д.).
- Перед новым command-trigger прогоном включён cleanup старых agent-owned comments, чтобы документ не засорялся прошлой выдачей.
- Подключена `review memory`: предыдущие findings, summaries, architectural notes и decisions теперь попадают в следующий прогон.
- Memory используется не только в prompt, но и в backend suppression для повторных итераций.
- Поднят `Qdrant` как отдельный контейнер, без встраивания в монолит.
- Подключён vector indexing через OpenRouter embeddings (`openai/text-embedding-3-small`).
- Векторная память реально пишет точки в `Qdrant` и не блокирует основной pipeline при сбоях.
- Добавлен единый `IssueFingerprint` для частых тематик и общий theme mapping для dedup и summary.
- Расширены fingerprints для:
  - refund threshold
  - partial refund rules
  - reason dictionary rules
  - concurrency / case assignment
  - audit consistency
  - billing idempotency
  - loading / empty / error states
  - notification retry
  - SLA / SLO
  - report / export
- Добавлен `role ownership`, чтобы типовые темы не дублировались между ролями без необходимости.
- Усилены role-prompts для `Tech Lead`, `Security Lead`, `DevOps Reviewer`, `Mobile Lead`.
- `DevOps` и `Mobile` теперь жёстче ограничены своей зоной ответственности.
- Summary теперь группирует замечания по общей taxonomy и меньше распадается на похожие темы.
- Примеры внутри summary теперь дедуплицируются по fingerprint, а не по сырым findings.
- Incremental suppression теперь работает по semantic-like fingerprint key, а не только по буквальному совпадению текста.
- В memory добавлены `ConsistencyHints`, чтобы LLM лучше сверяла новые фрагменты с уже найденными решениями и рисками.
- Добавлены и прогнаны регрессионные тесты для dedup, summary grouping, memory suppression и ownership.

## Inline Comments

- Попытка сделать `native inline comments` в `Google Docs` через публичные `Docs/Drive API` не дала надёжного результата.
- `Drive anchor` технически иногда записывается, но `Google Docs UI` не гарантирует ожидаемый inline UX.
- Для настоящих inline comments пришлось бы идти в недокументированные механизмы или в рискованные обходные сценарии.
- Это сознательно остановлено: текущая честная граница проекта — обычные document comments без гарантированного native inline positioning.
