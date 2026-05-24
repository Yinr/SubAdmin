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
- Import preview without upstream writes.
- Audit log recording and read-only audit view for key operations.
- Protected docs page for logged-in users.
- Next planned product work is import templates before any import execution.
