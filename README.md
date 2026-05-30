<<<<<<< HEAD
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
# Proofreading-Technical-Specifications



## Getting started

To make it easy for you to get started with GitLab, here's a list of recommended next steps.

Already a pro? Just edit this README.md and make it your own. Want to make it easy? [Use the template at the bottom](#editing-this-readme)!

## Add your files

* [Create](https://docs.gitlab.com/user/project/repository/web_editor/#create-a-file) or [upload](https://docs.gitlab.com/user/project/repository/web_editor/#upload-a-file) files
* [Add files using the command line](https://docs.gitlab.com/topics/git/add_files/#add-files-to-a-git-repository) or push an existing Git repository with the following command:

```
cd existing_repo
git remote add origin https://gitlab.com/appfox-ownership/agents/proofreading-technical-specifications.git
git branch -M main
git push -uf origin main
```

## Integrate with your tools

* [Set up project integrations](https://gitlab.com/appfox-ownership/agents/proofreading-technical-specifications/-/settings/integrations)

## Collaborate with your team

* [Invite team members and collaborators](https://docs.gitlab.com/user/project/members/)
* [Create a new merge request](https://docs.gitlab.com/user/project/merge_requests/creating_merge_requests/)
* [Automatically close issues from merge requests](https://docs.gitlab.com/user/project/issues/managing_issues/#closing-issues-automatically)
* [Enable merge request approvals](https://docs.gitlab.com/user/project/merge_requests/approvals/)
* [Set auto-merge](https://docs.gitlab.com/user/project/merge_requests/auto_merge/)

## Test and Deploy

Use the built-in continuous integration in GitLab.

* [Get started with GitLab CI/CD](https://docs.gitlab.com/ci/quick_start/)
* [Analyze your code for known vulnerabilities with Static Application Security Testing (SAST)](https://docs.gitlab.com/user/application_security/sast/)
* [Deploy to Kubernetes, Amazon EC2, or Amazon ECS using Auto Deploy](https://docs.gitlab.com/topics/autodevops/requirements/)
* [Use pull-based deployments for improved Kubernetes management](https://docs.gitlab.com/user/clusters/agent/)
* [Set up protected environments](https://docs.gitlab.com/ci/environments/protected_environments/)

***

# Editing this README

When you're ready to make this README your own, just edit this file and use the handy template below (or feel free to structure it however you want - this is just a starting point!). Thanks to [makeareadme.com](https://www.makeareadme.com/) for this template.

## Suggestions for a good README

Every project is different, so consider which of these sections apply to yours. The sections used in the template are suggestions for most open source projects. Also keep in mind that while a README can be too long and detailed, too long is better than too short. If you think your README is too long, consider utilizing another form of documentation rather than cutting out information.

## Name
Choose a self-explaining name for your project.

## Description
Let people know what your project can do specifically. Provide context and add a link to any reference visitors might be unfamiliar with. A list of Features or a Background subsection can also be added here. If there are alternatives to your project, this is a good place to list differentiating factors.

## Badges
On some READMEs, you may see small images that convey metadata, such as whether or not all the tests are passing for the project. You can use Shields to add some to your README. Many services also have instructions for adding a badge.

## Visuals
Depending on what you are making, it can be a good idea to include screenshots or even a video (you'll frequently see GIFs rather than actual videos). Tools like ttygif can help, but check out Asciinema for a more sophisticated method.

## Installation
Within a particular ecosystem, there may be a common way of installing things, such as using Yarn, NuGet, or Homebrew. However, consider the possibility that whoever is reading your README is a novice and would like more guidance. Listing specific steps helps remove ambiguity and gets people to using your project as quickly as possible. If it only runs in a specific context like a particular programming language version or operating system or has dependencies that have to be installed manually, also add a Requirements subsection.

## Usage
Use examples liberally, and show the expected output if you can. It's helpful to have inline the smallest example of usage that you can demonstrate, while providing links to more sophisticated examples if they are too long to reasonably include in the README.

## Support
Tell people where they can go to for help. It can be any combination of an issue tracker, a chat room, an email address, etc.

## Roadmap
If you have ideas for releases in the future, it is a good idea to list them in the README.

## Contributing
State if you are open to contributions and what your requirements are for accepting them.

For people who want to make changes to your project, it's helpful to have some documentation on how to get started. Perhaps there is a script that they should run or some environment variables that they need to set. Make these steps explicit. These instructions could also be useful to your future self.

You can also document commands to lint the code or run tests. These steps help to ensure high code quality and reduce the likelihood that the changes inadvertently break something. Having instructions for running tests is especially helpful if it requires external setup, such as starting a Selenium server for testing in a browser.

## Authors and acknowledgment
Show your appreciation to those who have contributed to the project.

## License
For open source projects, say how it is licensed.

## Project status
If you have run out of energy or time for your project, put a note at the top of the README saying that development has slowed down or stopped completely. Someone may choose to fork your project or volunteer to step in as a maintainer or owner, allowing your project to keep going. You can also make an explicit request for maintainers.
>>>>>>> gitlab/main
