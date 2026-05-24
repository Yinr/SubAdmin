# subAdmin Development Plan

## Product Direction

SubAdmin is a lightweight management panel for sub2api. It acts as a server-side BFF: the browser talks only to SubAdmin, and SubAdmin talks to sub2api admin APIs with credentials stored server-side.

The current product focus is safe read-only management, low-risk account maintenance, and auditable batch workflows. Destructive operations and broad account mutations must remain behind preview, confirmation, job tracking, and audit logs.

## Security Baseline

- Never expose sub2api admin keys to browser code, localStorage, logs, or API responses.
- Store sub2api admin keys encrypted in SQLite.
- Use HttpOnly session cookies for SubAdmin login sessions.
- Store only session token hashes in SQLite.
- Redact sensitive upstream account fields before returning account data to the browser.
- Keep high-risk account write operations disabled until preview, confirmation, jobs, and audit logs exist.
- Keep batch test response logs opt-in only and write them under `SUBADMIN_LOG_DIR`.

## Current Status

Status: usable for safe read-only management and low-risk account maintenance.

Implemented capabilities:

- Go backend serves the Vue SPA, protected API routes, and protected docs.
- SQLite stores sites, sessions, app settings, import-template schema, jobs, and audit-log schema.
- Single-secret login uses HttpOnly session cookies and hashed session tokens.
- Site management supports encrypted sub2api admin-key storage, CRUD, default-site selection, and connection tests.
- Top-level navigation includes Statistics, Accounts, Sites, Jobs, and Docs.
- Account listing is proxied server-side and sensitive fields are redacted before browser responses.
- Account filters support upstream query filters plus local current-page group/scheduling filters.
- Account UI supports desktop tables, mobile cards, detail modal, badges, usage rows, copy helpers, and batch selection.
- Batch account testing runs through persisted Jobs with progress, per-item results, optional sanitized response logs, failed-item retry, and best-effort cancellation.
- Batch token refresh runs through persisted Jobs, proxies sub2api's native batch-refresh endpoint with explicit confirmation, stores per-account outcomes, supports failed-item retry, and supports best-effort cancellation.
- Jobs view lists recent jobs, reopens batch-test/token-refresh results, retries failed items, and cancels queued/running jobs.
- Statistics dashboard uses real upstream dashboard and Ops endpoints, defaults to 24h/hourly, and supports isolated refresh for user/account concurrency panels.
- Application logs use structured JSON Lines with configurable level, request IDs, and simple file rotation under `SUBADMIN_LOG_DIR`.
- Import page supports pasted text or small files, parses known account-shaped content server-side, and returns a sanitized preview without upstream writes.
- Audit logs record site writes, job actions, and import preview summaries; the Audit page lists recent audit records.
- Protected OpenAPI, Swagger UI, and AI reference docs are available inside the logged-in UI.

## Completed Areas

The following areas are complete for the current scope and should only change for hardening, concrete UX issues, or upstream API changes:

- Foundations: backend entrypoint, SPA serving, SQLite bootstrap, config, session auth, protected routes.
- Site management: encrypted credentials, CRUD, default site, connection tests.
- Read-only account management: list, filters, redacted details, responsive UI, copy helpers.
- Low-risk batch maintenance: account testing and token refresh through Jobs.
- Jobs foundation: persisted job rows, progress/result JSON, retry failed, cancel queued/running, restart cleanup.
- Statistics: real dashboard/Ops data only, no mock-only fields.
- Protected docs: OpenAPI, Swagger UI, AI reference.

Keep completed implementation details out of this plan unless they affect future design decisions.

## Current Limitations

- `import_templates` table exists, but there is no complete UI/API workflow using it yet.
- Jobs are single-process goroutines; there is no distributed worker, durable queue, cron scheduler, or parallel execution model.
- Per-item job outcomes are stored in `result_json`; there is no `job_items` table yet.
- Filtered account ID collection is still frontend-driven because upstream and local filter semantics are not fully reproducible server-side.
- Token refresh cancellation is best-effort: SubAdmin can cancel local tracking/request state, but it cannot roll back upstream refresh work already performed by sub2api.
- High-risk account writes and import execution remain disabled until explicit confirmation and job execution are implemented on top of audit logging.

## Current Import Preview Scope

Import preview is implemented as a safe parsing and validation step only. It does not create accounts, refresh tokens, or call upstream write APIs.

Current scope:

- Accept pasted text and small uploaded files in known sub2api account formats.
- Parse input server-side only.
- Return recognized accounts, platform/type, display name, group hints, missing required fields, duplicate risks, and validation warnings.
- Allow draft import settings such as default group, proxy, priority, concurrency, and naming rules.
- Store no upstream credentials in browser storage.
- Do not call upstream write APIs.
- Do not create accounts.

## Planned After Import Preview

### Import Templates

- Store reusable import defaults in SQLite.
- Apply templates during import preview.
- Keep templates server-side and avoid exposing sensitive credentials.

### Import Execution

- Only start after import preview and audit logs are in place.
- Require explicit confirmation.
- Execute through persisted Jobs.
- Store per-item outcomes.
- Support retry where safe.
- Never bypass audit logging for upstream writes.

### High-Risk Account Operations

- Keep disabled until preview, confirmation, Jobs, and audit logs are all in place.
- Add operations incrementally and only with clear rollback/error semantics.

## Hardening Backlog

- Move job code out of `cmd/subadmin/main.go` if it grows further.
- Add `job_items` only if `result_json` becomes too large or hard to query.
- Add targeted backend tests for auth, encryption, redaction, proxy behavior, Jobs, and import preview parsing.
- Add upload size and batch size limits for import preview.
- Improve operational error messages around upstream timeouts and partial failures.
- Revisit log rotation/retention policy if long-running deployments need time-based retention or compression.
- Refine dense UI areas only when concrete usability issues appear.

## Immediate Next Step

Add import templates, then design confirmed import execution through Jobs without bypassing audit logging.
