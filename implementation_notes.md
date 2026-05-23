# Implementation Notes

## Goal

Build an AI document review system based on the test assignment:

- ingest a document;
- analyze it with an LLM as a senior technical reviewer;
- detect ambiguities, contradictions, missing requirements, and technical risks;
- produce structured findings;
- optionally publish comments back to Google Docs.

The first target is the original technical specification review flow. Additional document types can be added later.

## Product Direction

We keep the core aligned with the assignment:

- primary scenario: `Technical Specification Review Agent`;
- first interface can be minimal;
- frontend is not the priority;
- backend and analysis pipeline are the core value.

Possible extension after MVP:

- Telegram bot using the same backend;
- richer React admin panel;
- extra document types such as resume analysis.

## Chosen Backend Direction

Backend will be implemented as a Go monolith.

Reasons:

- simple deployment;
- low memory usage;
- clean layered architecture;
- good fit for API + jobs + integrations in one service.

## High-Level Flow

1. User uploads a file or provides a Google Docs source.
2. Backend fetches and parses the document.
3. Text is split into chunks.
4. Context is stored for cross-block review.
5. LLM analysis runs with system-prompt-driven logic.
6. Findings are normalized into structured output.
7. Results are saved in storage.
8. Comments can be published to Google Docs or shown in UI.

## LLM Logic

The LLM runs on the backend.

Expected analysis modes:

- full document review;
- incremental review by block or section;
- role-based review perspective:
  - backend lead;
  - frontend lead;
  - mobile lead;
  - devops lead;
  - QA lead;
  - security lead.

Each finding should follow a strict structure:

- `problem`
- `why_it_is_bad`
- `how_to_fix`
- `severity`
- `category`
- `role`

Severity levels:

- `INFO`
- `WARNING`
- `ERROR`
- `CRITICAL`

## Suggested Core Components

- `API`
- `Document Ingestion`
- `Parser`
- `Chunking Engine`
- `Context Memory`
- `LLM Orchestrator`
- `Findings Formatter`
- `Google Docs Comment Publisher`
- `Storage Layer`

## Storage

Suggested minimum:

- `PostgreSQL` for documents, runs, findings, and history;
- `Redis` for cache and async jobs;
- file storage local or S3-compatible for MVP.

Vector DB is optional and not required for MVP.

## Infra Notes

The test assignment does not require a specific hosting platform.

Allowed practical direction:

- deploy backend to a small VPS or microserver;
- run as Docker container;
- keep infra simple for MVP.

Initial server target:

- `2 vCPU`
- `2 GB RAM`

This should be enough for MVP because the main latency comes from external LLM calls, not raw backend compute.

## Performance Interpretation

The milliseconds listed in the PDF are examples of clarified requirements, not strict SLA for the AI agent.

Examples from the document:

- list `<= 2s`
- search `<= 500ms`
- API response `<= 300ms`

For this project the correct backend approach is:

- fast API for job creation and status polling;
- asynchronous document analysis;
- result retrieval after processing completes.

LLM analysis time depends on:

- provider latency;
- document size;
- number of chunks;
- number of review passes.

## Frontend Notes

UI is not explicitly specified in the assignment.

That means:

- UI can be designed freely;
- backend correctness matters more than interface polish in the first stage.

Planned frontend direction later:

- React admin panel;
- drag-and-drop upload;
- document list;
- analysis status view;
- findings and summary screen.

## MVP Scope

Must have:

- document upload or Google Docs ingestion;
- parsing and chunking;
- LLM review pipeline;
- structured findings;
- incremental review support;
- context memory;
- optional Google Docs comment publishing.

Nice to have later:

- Telegram bot;
- polished React UI;
- multi-document review types;
- diagram generation;
- auto-fix draft.
