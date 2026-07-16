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
- Keep high-risk account write operations behind preview, explicit confirmation, Jobs, and audit logs.
- Keep batch test response logs opt-in only and write them under `SUBADMIN_LOG_DIR`.

## Current Status

Status: usable for safe read-only management, low-risk account maintenance, and confirmed account import workflows.

Implemented capabilities:

- Go backend serves the Vue SPA, protected API routes, and protected docs.
- SQLite stores sites, sessions, app settings, import-template schema, jobs, and audit-log schema.
- Single-secret login uses HttpOnly session cookies and hashed session tokens.
- Site management supports encrypted sub2api admin-key storage, CRUD, default-site selection, and connection tests.
- Top-level navigation includes Statistics, Accounts, Import, Jobs, Audit, Sites, and Docs.
- Account listing is proxied server-side and sensitive fields are redacted before browser responses.
- Account filters support upstream query filters plus local current-page group/scheduling filters.
- Account UI supports desktop tables, mobile cards, detail modal, badges, usage rows, copy helpers, and batch selection.
- Batch account testing runs through persisted Jobs with progress, per-item results, optional sanitized response logs, failed-item retry, and best-effort cancellation.
- Batch token refresh runs through persisted Jobs, proxies sub2api's native batch-refresh endpoint with explicit confirmation, stores per-account outcomes, supports failed-item retry, and supports best-effort cancellation.
- Jobs view lists recent jobs, reopens batch-test/token-refresh results, retries failed items, and cancels queued/running jobs.
- Statistics dashboard uses real upstream dashboard and Ops endpoints, defaults to 24h/hourly, and supports isolated refresh for user/account concurrency panels.
- Application logs use structured JSON Lines with configurable level, request IDs, and simple file rotation under `SUBADMIN_LOG_DIR`.
- Import page supports pasted text or small files, parses known account-shaped content server-side, and returns a sanitized preview with warnings, duplicate-risk markers, and paged results. Confirmed imports run through Jobs and call sub2api's `/api/v1/admin/accounts/batch`; selected model lists are written as account `credentials.model_mapping` identity mappings. Real multi-batch imports have been exercised successfully.
- Import templates can save, overwrite, delete, and apply non-sensitive preview defaults.
- Audit logs record site writes, job actions, import preview summaries, and import-template create/delete actions; the Audit page lists recent audit records.
- Protected OpenAPI, Swagger UI, and AI reference docs are available inside the logged-in UI.
- Documentation has been refreshed to track sub2api `v0.1.133`; backend or frontend behavior changes still need explicit verification before follow-up work.
- Frontend structure now separates top-level App orchestration from Import, Audit, and Docs views, with shared helpers for API calls, formatting, visual metrics, import settings, and account display.
- Account test hint recognition covers token revocation, token invalidation, deactivated workspace, network/timeout errors, upstream auth failures, Cloudflare blocks, upstream server errors, and rate-limit/quota exhaustion with plan type and reset time.
- Batch test default model pre-fills gpt-5.5; clearing the field falls back to sub2api platform defaults.
- Search-by-names API and SearchView support batch keyword account lookup with per-keyword error reporting and truncation metadata.
- Error accounts API and ErrorAccountsView list status=error accounts by group with local handled/unhandled tracking and email copy helpers.
- Group quota API and GroupQuotaView estimate per-account quota using usage API cycle costs, with 24h cache, bucket distribution, and sortable account table.
- Shared CSS (shared.css) provides sa-* utility classes for view components.

## Completed Areas

The following areas are complete for the current scope and should only change for hardening, concrete UX issues, or upstream API changes:

- Foundations: backend entrypoint, SPA serving, SQLite bootstrap, config, session auth, protected routes.
- Site management: encrypted credentials, CRUD, default site, connection tests.
- Read-only account management: list, filters, redacted details, responsive UI, copy helpers.
- Low-risk batch maintenance: account testing and token refresh through Jobs.
- Jobs foundation: persisted job rows, progress/result JSON, retry failed, cancel queued/running, restart cleanup.
- Statistics: real dashboard/Ops data only, no mock-only fields.
- Confirmed account import: preview, explicit confirmation, persisted Jobs, batch upstream creation, audit logging, and non-sensitive templates.
- Read-only audit/docs pages: audit log list and protected documentation view.
- Protected docs: OpenAPI, Swagger UI, AI reference.

Keep completed implementation details out of this plan unless they affect future design decisions.

## Current Limitations

- Import templates cover non-sensitive import defaults only; they do not store account credentials.
- Jobs are single-process goroutines; there is no distributed worker, durable queue, cron scheduler, or parallel execution model.
- Per-item job outcomes are stored in `result_json`; there is no `job_items` table yet.
- Filtered account ID collection is still frontend-driven because upstream and local filter semantics are not fully reproducible server-side.
- Token refresh cancellation is best-effort: SubAdmin can cancel local tracking/request state, but it cannot roll back upstream refresh work already performed by sub2api.
- Import execution uses single upstream batch requests, so cancellation can prevent queued/local tracking only and cannot roll back upstream account creation already accepted by sub2api.

## Current Import Scope

Import starts with server-side preview, then requires explicit target-site confirmation before creating a persisted Job. This workflow has been used successfully for multiple real import batches; recent logs show successful `import_accounts` jobs including batches of 103 accounts and smaller follow-up batches.

Current scope:

- Accept pasted text and small uploaded files in known sub2api account formats.
- Parse input server-side only.
- Return recognized accounts, platform/type, display name, group hints, missing required fields, duplicate risks, and validation warnings.
- Allow import settings such as existing groups, existing proxy, priority, concurrency, naming rules, and model list.
- Render `{date}` in import name prefixes as `YYYYMMDD` during preview and execution.
- Execute confirmed imports through sub2api's batch account creation endpoint.
- Store selected models in `credentials.model_mapping` using identity mapping.
- Store no upstream credentials in browser storage.
- Store no account credentials in job input, job result, audit logs, or application logs.

### High-Risk Account Operations

- Keep new high-risk operations disabled until preview, confirmation, Jobs, and audit logs are all in place.
- Add operations incrementally and only with clear rollback/error semantics.
- Existing confirmed import execution remains enabled because it now satisfies the preview/confirmation/Jobs/audit baseline.

## Hardening Backlog

- Move job code out of `cmd/subadmin/main.go` if it grows further.
- Add `job_items` only if `result_json` becomes too large or hard to query.
- Add targeted backend tests for auth, encryption, redaction, proxy behavior, Jobs, and import preview parsing.
- Improve operational error messages around upstream timeouts and partial failures.
- Improve import job result summaries when upstream returns partial failures or non-standard batch responses.
- Revisit log rotation/retention policy if long-running deployments need time-based retention or compression.
- Refine dense UI areas only when concrete usability issues appear.
- Continue frontend view extraction only when it clearly reduces risk; defer splitting the Accounts view until account workflows stabilize further.

## Immediate Next Step

Run a focused regression pass after the recent frontend view split, then harden import failure diagnostics only when real failure cases or unclear upstream responses appear.
