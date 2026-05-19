# subAdmin Development Plan

## Phase 0: Foundations
- Create repository structure for backend, frontend, docs, and persistent data.
- Add Go module and initial server entrypoint.
- Add Vue frontend scaffold and build pipeline.
- Add SQLite database bootstrap and migrations.
- Add session-cookie authentication.
- Add encrypted site credential storage.
- Add protected docs serving.

## Phase 1: Site Management
- CRUD for sub2api site records.
- Store base URL, encrypted admin key, note, and enabled/default flags.
- Add connection test for each site.
- Add site switching in the UI.

Status:

- Backend site CRUD APIs added.
- Site admin keys are encrypted before storage.
- Site connectivity test API added.
- UI site management panel added in the shell.

## Phase 2: Account Listing
- List upstream accounts for the selected site.
- Add filtering by group, status, schedule, proxy, priority, rate, last used, and expiry.
- Add text search for name and note.
- Add pagination and account detail view.

Status:

- Backend account-list proxy added for each site.
- Minimal account query UI added with search, platform, and status fields.
- Advanced filters and pagination controls are still pending.

## Phase 3: Import Workflow
- Accept uploads of sub2api-supported upstream account formats.
- Parse and preview imported data before commit.
- Support import templates for model, proxy, note, group, priority, concurrency, and name rule.
- Add import job tracking and results.

## Phase 4: Account Management
- Batch update selected or filtered accounts.
- Support change of group, status, schedule, proxy, priority, rate, name, note.
- Support refresh usage window, connection test, and delete.
- Add audit logs for destructive and bulk actions.

## Phase 5: Docs Integration
- Serve protected OpenAPI and AI reference docs inside the app.
- Remove dependency on separate docs deployment.

## Phase 6: Hardening
- Add operational logs and better error handling.
- Tune upload and batch limits.
- Add usability polish for the UI.

## Notes
- Keep the first version simple and low-overhead.
- Prefer server-side aggregation and filtering where possible.
- Revisit schema only when a real need appears.
