# subAdmin Agent Guide

This directory contains the management application for sub2api.

## Goals
- Build a lightweight admin web app with a Go backend and a modern Vue frontend.
- Keep all `sub2api` admin-key usage on the server side only.
- Store persistent data in SQLite.
- Embed protected API docs into the app for logged-in users.

## Working Rules
- Prefer the smallest correct change.
- Keep runtime overhead low.
- Do not expose `sub2api` admin keys to browser code, localStorage, or client-side logs.
- Use session-cookie auth for the management app.
- Use encrypted storage for sensitive site credentials.
- Keep the UI modern and practical, but avoid unnecessary complexity.
- If a change affects server behavior, verify it with a quick build or targeted run.

## Architecture Preferences
- Go backend serves the SPA and all protected API endpoints.
- Vue 3 frontend is built as static assets and served by the Go backend.
- SQLite stores sites, sessions, app settings, templates, jobs, and audit logs.
- API docs under `subAdmin/docs` are a source of truth and can be embedded into the app.
- Batch test response logs are opt-in only and should use the project-root log directory configured by `SUBADMIN_LOG_DIR`.

## Security Rules
- Never send the `sub2api` admin key to the browser.
- Use `HttpOnly`, `Secure`, `SameSite=Lax` cookies for sessions.
- Store session tokens as hashes, not plaintext.
- Encrypt site admin keys at rest.

## Version Control Discipline

- **Clean workspace before new work.** Before starting a new feature or fix, ensure the working tree is clean. If dirty, evaluate existing changes: commit them if they form a coherent unit, otherwise stash or branch to prevent unrelated changes from mixing. When uncertain, ask the user how to proceed.
- **Commit when coherent.** As soon as a set of changes is complete and self-consistent, commit and push. Do not accumulate unrelated changes across features or fixes.
- **Pre-commit checklist.** Before every commit:
  1. Build compiles (`go build`) and tests pass (`go test`).
  2. Frontend builds without errors (`npm run build`) if web sources changed.
  3. Related documentation (`PLAN.md`, `AGENTS.md`, `docs/`) is updated to reflect the change.
  4. No sensitive data (secrets, local paths, personal info) in the diff or commit message.
  5. Commit message follows conventional format: `type: concise description` (types: feat, fix, docs, chore, refactor, test).
  6. Push immediately after committing.
- **Incomplete work documentation.** If a change cannot be finished in the current session, record its state in `PLAN.md` under a dedicated section or in a `WIP.md` file at the project root. Include: the goal of the change, what has been done, what remains, and where the work is blocked. This ensures the next session or agent can understand the workspace state and decide whether to commit the partial work.

## Implementation Notes
- Keep files ASCII unless the file already uses other characters.
- Use `apply_patch` for file edits.
- Prefer simple, direct code paths over abstraction early on.
- Update `PLAN.md` when milestones are completed or scope changes.

## Current Scope
- Login by single management secret.
- Multi-site switching.
- Account listing, filtering, batch testing, and batch token refresh.
- Statistics dashboard based on real upstream dashboard and Ops data.
- Persisted task implementation for batch workflows, including retry and best-effort cancellation.
- Import preview without upstream writes, followed by confirmed import execution through Jobs.
- Import templates for non-sensitive preview defaults.
- Audit log recording and read-only audit view for key operations.
- Protected docs page for logged-in users.
- Next planned product work is focused regression, import failure diagnostics, and hardening.
