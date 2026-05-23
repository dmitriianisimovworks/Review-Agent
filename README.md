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
