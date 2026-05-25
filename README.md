# Technical Specification Review Agent

Backend-first monolith scaffold for the test assignment.

## Structure

```text
.
├── apps
│   └── web                  # Future React / Next.js frontend
├── cmd
│   └── api                  # Application entrypoint
├── internal
│   ├── api                  # HTTP router, handlers, middleware, DTOs
│   ├── app                  # Wiring and bootstrap
│   ├── config               # Environment-based config
│   ├── domain               # Core business models
│   ├── integration          # External integrations (LLM, Google)
│   ├── jobs                 # Async job contracts / workers
│   ├── parser               # Document parsing contracts
│   ├── repository           # Persistence contracts and implementations
│   └── service              # Application services / orchestration
├── technical_specification_review_agent.pdf
└── implementation_notes.md
```

## Run

```bash
go run ./cmd/api
```

Default server address: `:8080`

## Docker

Local config is expected in `.env`.

```bash
docker compose up -d --build
docker compose ps
```

The compose stack uses an internal-only Docker network and does not publish ports externally yet.

## Google Docs ingestion

`POST /api/v1/analyses` also supports Google Docs as a source:

```json
{
  "source": "google_docs",
  "mode": "full_review",
  "google_doc_url": "https://docs.google.com/document/d/your-doc-id/edit"
}
```

For repeated reviews and incremental review, the backend now keeps a review memory thread:

- Google Docs automatically use the same thread via the document external ID.
- Uploads can provide an explicit `context_key` to reuse history between iterations.

Example incremental review for uploads:

```json
{
  "name": "billing-spec-v2.md",
  "source": "upload",
  "mode": "incremental_review",
  "context_key": "billing-service-spec",
  "content": "updated section text here"
}
```

In `incremental_review`, the prompt receives prior findings, prior summaries, and architectural notes from previous runs in the same review thread. The backend also suppresses exact known duplicates from previous iterations.

## Review Config

The backend also supports an optional project-level review config file:

```yaml
review:
  architecture: true
  backend: true
  frontend: true
  devops: true
  qa: true

severity:
  critical_block_merge: true

comments:
  inline: true
  summary: true

context:
  memory_enabled: true

document:
  chunk_size: 5000

llm:
  temperature: 0.3
  top_p: 0.8
  max_tokens: 1100
```

Place it at `.ai-spec-review.yml` in the working directory. This config controls review behavior only:

- enabled reviewer roles;
- default publish strategy for comments;
- whether review memory is enabled;
- document chunk size for analysis.
- generation settings for the LLM request.

Infrastructure and secrets remain environment-based.

The backend reads the document through a Google service account configured via:

- `GOOGLE_SERVICE_ACCOUNT_FILE`
- `GOOGLE_INBOX_FOLDER_ID` (optional inbox folder for auto-review flow)
- `GOOGLE_INBOX_POLL_SECONDS` (optional poll interval)
- `GOOGLE_OAUTH_CLIENT_ID`
- `GOOGLE_OAUTH_CLIENT_SECRET`
- `GOOGLE_OAUTH_REDIRECT_URL`
- `GOOGLE_OAUTH_SCOPES`

For the OAuth2 user flow, `GOOGLE_OAUTH_SCOPES` must include both Docs/Drive access and user identity scopes, for example:

```env
GOOGLE_OAUTH_SCOPES=https://www.googleapis.com/auth/documents,https://www.googleapis.com/auth/drive,https://www.googleapis.com/auth/userinfo.email,https://www.googleapis.com/auth/userinfo.profile
```
