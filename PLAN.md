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
- Batch test response logs are opt-in only and must be written under `SUBADMIN_LOG_DIR`.

## Current Capabilities

Status: usable for current read-only and low-risk maintenance workflows.

- Go backend serves the Vue SPA, protected API routes, and protected docs.
- SQLite stores app data, site records, sessions, templates, jobs, and audit-log tables.
- Single-secret login uses HttpOnly session cookies.
- Site management supports encrypted sub2api admin-key storage, CRUD, default-site selection, and connection tests.
- Top-level navigation includes Statistics, Accounts, Sites, and Docs.
- A shared active-site context is used by Statistics and Accounts.
- Account listing is proxied server-side and sensitive fields are redacted before browser responses.
- Account filters support upstream query filters plus local current-page group/scheduling filters.
- Account table/card UI is usable on desktop and mobile with detail modal, badges, chips, usage rows, and copy helpers.
- Batch account testing runs through SubAdmin's server-side proxy, parses known SSE result formats, supports filtered selections, delay/jitter, retry helpers, and optional sanitized response logs.
- Batch token refresh proxies sub2api's native batch-refresh endpoint with explicit confirmation.
- Statistics dashboard uses real upstream dashboard and Ops endpoints where available, without fabricating missing data.
- Protected docs are available inside the logged-in UI.

## Completed Scope Notes

### Foundations And Site Management

Status: complete for current scope.

The backend, frontend, SQLite bootstrap, encrypted site credentials, session auth, static serving, and multi-site CRUD are in place. No near-term feature work is planned here beyond hardening.

### Account Listing And Read-Only Operations

Status: complete for current scope.

The account page is usable for listing, filtering, inspecting redacted details, selecting accounts, and launching supported batch actions. Future work should be driven by concrete UX pain rather than more table polish.

### Protected Docs

Status: complete for current scope.

OpenAPI, Swagger UI, and AI reference docs are available behind SubAdmin login.

### Recent Status Statistics

Status: complete for current scope.

The statistics page now covers the useful real data that sub2api exposes reliably: dashboard snapshot, trends, model distribution, user ranking, user concurrency, and account concurrency. Previously considered realtime fields that are unavailable, mock-only, or unreliable should not be shown unless upstream support changes.

## Next Priority: Persisted Jobs

Status: design next, implementation after review.

Persisted Jobs turn long or batch operations from front-end-only transient actions into server-tracked records. The immediate target is batch account testing; the same structure should later support batch refresh, import preview/execution, and higher-risk account operations.

### Goals

- Keep batch workflows visible after refresh or navigation.
- Store progress, result summaries, and per-item outcomes in SQLite.
- Allow safe retry of failed items.
- Provide a foundation for future audit logs and high-risk operations.
- Keep the first version simple and single-process; do not introduce a queue service.

### Non-Goals For First Version

- No distributed workers.
- No cron scheduler.
- No parallel execution by default.
- No destructive account mutations.
- No long-term analytics over job history.

### Job Types

- `batch_account_test`: first implementation target.
- `batch_token_refresh`: later migration from current immediate API.
- `import_preview`: later, for upload/paste parsing without upstream writes.
- `import_execute`: later, only after preview, confirmation, and audit logs exist.

### Data Model

Use the existing `jobs` table if its schema is sufficient; otherwise add the smallest migration needed.

Minimum job fields:

- `id`: integer primary key.
- `type`: job type, such as `batch_account_test`.
- `status`: `queued`, `running`, `succeeded`, `failed`, `cancelled`.
- `site_id`: target site id.
- `created_at`, `started_at`, `finished_at`.
- `total_count`, `done_count`, `success_count`, `failure_count`.
- `input_json`: sanitized job input, including account ids and options.
- `summary_json`: sanitized final summary.
- `error`: top-level failure reason.

Per-item results can start as JSON in `summary_json` for the first implementation. If result size or querying becomes painful, add a `job_items` table with `job_id`, `target_id`, `status`, `duration_ms`, `result_json`, and `error`.

### API Design

- `POST /api/sites/{siteId}/jobs/batch-account-test`: create and start a batch test job.
- `GET /api/jobs`: list recent jobs, newest first.
- `GET /api/jobs/{id}`: get job details, progress, and per-item results.
- `POST /api/jobs/{id}/retry-failed`: create a new job from failed item ids.
- `POST /api/jobs/{id}/cancel`: best-effort cancellation for queued/running jobs.

### Execution Model

- First version can run in-process in a goroutine after creating the job row.
- Use a context/cancel registry keyed by job id for best-effort cancellation while the process is alive.
- Mark interrupted `running` jobs as `failed` on startup with an `interrupted by restart` error.
- Execute account tests sequentially with the same conservative delay/jitter behavior already used by the frontend.
- Persist progress after each item so refresh does not lose state.

### UI Design

- Add a small Jobs view or account-page Jobs panel.
- Show recent jobs with type, site, status, progress, created time, and final counts.
- Job detail shows item rows similar to the current batch test results table.
- Keep current immediate batch-test UI initially, but route execution through Jobs.
- Provide retry failed items from a completed failed/partial job.

### Safety Rules

- Store only sanitized inputs and outputs.
- Do not store upstream credentials or raw sensitive response fields.
- Keep response-log saving opt-in and separate from job persistence.
- Require explicit confirmation for token refresh and future write operations.
- Do not add high-risk writes until audit logs are implemented.

### Implementation Steps

1. Inspect current `jobs` schema and decide whether a migration is needed.
2. Add a small backend job repository/service.
3. Implement `batch_account_test` job creation, execution, progress updates, and detail retrieval.
4. Update the frontend batch test flow to create a job and poll job status.
5. Add retry-failed support by creating a new job from failed item ids.
6. Add startup cleanup for interrupted jobs.

## Later Planned Work

### Import Preview Workflow

Status: planned after Jobs foundation.

- Accept uploaded files and pasted text in known sub2api account formats.
- Parse input server-side and return a preview only.
- Show recognized accounts, platform/type, missing fields, duplicate risks, and validation warnings.
- Apply draft import settings such as default group, proxy, priority, concurrency, and naming rules.
- Do not call upstream write APIs until explicit confirmation and job/audit infrastructure exists.

### Import Templates

Status: planned.

- Store reusable import templates in SQLite.
- Use templates during import preview before any write operation is allowed.

### Audit Logs And High-Risk Operations

Status: planned and required before risky write operations.

- Record site changes, import actions, account write operations, and job actions.
- Include action, site, target identifiers, timestamp, session/IP metadata, and result.
- Require preview, confirmation, job tracking, and audit entries before high-risk account mutations.

### Hardening And Polish

Status: ongoing.

- Improve operational errors and validation.
- Add upload and batch-size limits where needed.
- Add targeted backend tests for auth, encryption, redaction, proxy behavior, and Jobs.
- Refine dense UI areas only when concrete usability issues appear.

## Immediate Next Step

Design review complete, then implement the minimum persisted Jobs path for `batch_account_test`.
