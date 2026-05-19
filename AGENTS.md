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
- Account import, listing, filtering, and batch actions.
- Protected docs page for logged-in users.
