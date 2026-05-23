# subAdmin Development Plan

## Product Direction

SubAdmin is a lightweight management panel for sub2api. It acts as a server-side BFF: the browser talks only to SubAdmin, and SubAdmin talks to sub2api admin APIs with credentials stored server-side.

The near-term priority is a safe read-only management experience plus low-risk account maintenance. Destructive operations and account mutations come later and must include preview, confirmation, job tracking, and audit logs.

## Security Baseline

- Never expose sub2api admin keys to browser code, localStorage, logs, or API responses.
- Store sub2api admin keys encrypted in SQLite.
- Use HttpOnly session cookies for SubAdmin login sessions.
- Store only session token hashes in SQLite.
- Redact sensitive upstream account fields before returning account data to the browser.
- Keep account write operations disabled until preview, confirmation, jobs, and audit logs exist.

## Phase 0: Foundations

Status: complete.

- Repository structure for backend, frontend, docs, and persistent data.
- Go backend entrypoint and config loading.
- SQLite bootstrap and initial schema.
- Session-cookie authentication.
- Vue 3 + Vite frontend build pipeline.
- Static frontend serving from Go backend, with fallback page when no build exists.
- sub2api source included as a submodule.

## Phase 1: Site Management

Status: complete for current scope.

- Site CRUD APIs and UI.
- Encrypted site admin-key storage.
- Admin-key hint only in API responses; plaintext keys are never returned.
- Site connection test through server-side sub2api admin API calls.
- Multi-site switching.
- Modal save UX hardened so backdrop clicks do not discard edits.
- Loading states for login, site loading, account loading, and site saves.

## Phase 2: Account Listing And Read-Only Operations

Status: in progress, mostly usable.

Completed:

- Read-only account-list proxy per site.
- Server-side sensitive field redaction for account-list responses.
- Account table with name, platform/status, groups, proxy/scheduling state, usage, and last-used time.
- Pagination with page-size selector, total pages, unavailable-next-page handling, and page-size reset to page 1.
- Short client-side cache for account queries.
- Race protection so slow responses do not overwrite newer results.
- Account detail modal based on the current list result, with redacted JSON.
- Fixed option filters for platform, status, type, privacy mode, sorting, page size, and scheduling state.
- Read-only groups proxy per site.
- Upstream single-group filtering sends sub2api's supported `group` query parameter before local filtering.
- Multi-group local tri-state filtering on the current result set:
  - include: account must contain the group.
  - exclude: account must not contain the group.
  - ignored: group is not part of the filter list.
- Lightweight dashboard overview cards based on loaded site/account data.
- Account detail modal now has clearer read-only sections for basic info, groups/proxy, scheduling, usage, and errors.
- Filter UI now separates upstream query filters from current-page local filters.
- Copy helpers added for account ID, account name, and active filter summary.

Next:
- Account maintenance: batch testing for selected accounts.
- Account table polish: replace dense text cells with structured badges, chips, compact metadata rows, and clearer status/usage/scheduling components.
- Add account table ergonomics such as column visibility, sticky key columns, better mobile layout, and clearer empty/loading/error states.

## Phase 3: Protected Docs Integration

Status: complete for current scope.

- Protected `/docs/` route added for existing Swagger UI assets.
- Frontend overview card links to Swagger UI, OpenAPI YAML, and AI Reference.
- Logged-in docs page added inside the SubAdmin UI.
- AI Reference is fetched through the protected docs route and rendered in-app.
- Separate docs deployment is no longer required for current bundled docs.

## Phase 4: Account Maintenance - Batch Testing

Status: in progress. This phase is read-only from SubAdmin's perspective and reuses upstream account test endpoints.

Completed:

- Select accounts from the current account table.
- Run batch tests for selected accounts.
- Reuse sub2api's existing single-account test endpoint for each account.
- Keep a conservative batch limit of 20 accounts and sequential execution in the first version.
- Show per-account test results with success/failure, status code, duration, and sanitized response text.
- Batch test options include optional model id, prompt, and mode; empty model uses sub2api's default test model.
- Batch test results now parse upstream SSE events to report success/error and tested model.
- Frontend runs selected account tests one by one and updates each row as it completes.
- Batch test result helpers added for copying failed account IDs and retrying failed items only.
- Batch test result parser recognizes known rate-limit signals such as 429, usage_limit_reached, resets_at, retry_after, and rate_limit_reset_at.
- Batch test parser now handles OpenAI-style 429 usage limit payloads with `plan_type`, Unix `resets_at`, and `resets_in_seconds`.
- Batch testing supports configurable per-account delay with random jitter for smoother upstream load.
- Batch testing auto-raises the minimum inter-request delay for large selections.
- Batch testing can collect and test all accounts matching the current upstream and local filters, not only the current page.
- Batch token refresh proxies sub2api `/api/v1/admin/accounts/batch-refresh` for selected or filtered accounts with explicit confirmation.
- Batch operation panels now appear before the account table with progress bars, bounded scroll areas, auto-scroll, and cached filtered ID sets.
- Batch test response logs are off by default; when enabled, sanitized logs can be written under the project-root log directory configured by `SUBADMIN_LOG_DIR`.

Next:

- Promote batch tests into persisted Jobs with retry and history.
- Optional later polish after core workflows are stable: export batch test results, retry a single failure category, and expand known-error classification for clearer summaries.

## Phase 4.5: Recent Status Statistics

Status: next priority.

Navigation model:

- Split site selection/settings from account operations.
- Keep a shared active site context used by the statistics page and account management page.
- If a default site exists, enter the statistics page directly after login and use that default site.
- If no default site exists, route the user to site selection/settings first.
- Account management should use the shared active site and only query accounts when the user explicitly clicks query/refresh.

- Add a status statistics page/panel for recent runtime activity.
- Show active users and each user's concurrent connection count.
- Show active upstream accounts and each upstream account's concurrent usage count.
- Show recent user usage summaries with 1/5/10/30 minute and 1 hour windows.
- Prefer server-side aggregation so the browser does not need to pull excessive raw usage rows.
- Add top-level navigation for Statistics, Accounts, Sites/Settings, and Docs.

## Phase 5: Import Preview Workflow

Status: planned. Preview first, no upstream writes initially.

- Accept uploaded files and pasted text in sub2api-supported upstream account formats.
- Parse input server-side and return a preview only.
- Show recognized accounts, platform/type, missing fields, duplicate risks, and validation warnings.
- Apply draft import settings such as default group, proxy, priority, concurrency, and naming rules.
- Do not call upstream write APIs until explicit confirmation and job/audit infrastructure exist.

## Phase 6: Import Templates

Status: planned.

- Store reusable import templates in SQLite.
- Template fields may include default platform, account type, group, proxy, priority, concurrency, notes, and naming rules.
- Use templates during import preview before any write operation is allowed.

## Phase 7: Jobs And Audit Logs

Status: planned and required before risky write operations.

- Jobs page for import previews, import execution, and batch operations.
- Audit logs for site changes, import actions, and account write operations.
- Record action, site, target identifiers, timestamp, session/IP metadata, and result.
- Keep audit records for failed and cancelled operations too.

## Phase 8: Account Management And Batch Operations

Status: planned after Phase 6.

Low-risk candidates:

- Refresh account status or usage.
- Clear account error state.
- Clear rate limits.

High-risk candidates:

- Change groups, status, schedule, proxy, priority, rate, name, or note.
- Delete accounts.
- Batch update selected or filtered accounts.

Requirements for high-risk operations:

- Preview affected accounts.
- Explicit confirmation.
- Job tracking.
- Audit log entry.
- Conservative limits and clear error reporting.

## Phase 9: Hardening And Polish

Status: ongoing.

- Better operational logs and error messages.
- Upload and batch-size limits.
- UI polish for mobile and dense data tables.
- Account table redesign from text-heavy cells into structured visual components:
  - platform/type/status badges;
  - group chips with overflow handling;
  - proxy and scheduling badges;
  - usage meters or compact usage rows;
  - last-used and error summaries with consistent formatting;
  - optional column visibility and sticky identity/action columns.
- Server-side aggregation where it reduces data transfer or makes filtering more correct.
- Test coverage for auth, credential encryption, redaction, and proxy behavior.

## Immediate Next Step

Implement the shared active-site navigation model, then build the recent status statistics page/panel.
