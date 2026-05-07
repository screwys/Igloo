# Igloo Agent Guide

## Project

- Igloo is a Go/SQLite server with web and Android clients.
- `android/` is the current Android app.
- Runtime/config defaults: native `~/.local/share/igloo/` and `~/.config/igloo/`; container `/data` and `/config`; bundled container assets `/app/static`.

## Evidence

- Start from local evidence: files, DB rows, logs, running DOM, device/app state, then code.
- Inspect the DB read-only when possible:
  `sqlite3 "file:$HOME/.local/share/igloo/igloo.db?mode=ro"`
- Do not fetch public X, YouTube, TikTok, or Instagram pages when stored identifiers or local data can answer the question.
- Check private runtime material only for existence; mask values as `***` if a format check is unavoidable.

## Coding Rules

- Keep changes scoped. Do not mix unrelated cleanup, formatting, generated churn, or private workflow notes into product work.
- Use generic names in tests, docs, examples, comments, and commits.
- Do not clear local state before a network call succeeds.
- Destructive UI actions need product confirmation: Igloo modal on web, Compose `AlertDialog` on Android.
- One-off repair/backfill utilities must not become normal startup behavior.
- Fix root causes, not display-only symptoms.
- If multiple causes are found, fix all in the same pass unless the user narrows the scope.
- Do not invent client-side fallbacks for server-owned identity or ingest-time data before tracing why the real data is missing.
- Keep status updates factual: what is fixed, what is still broken, and what is being worked on next.

For Go code, protect the success path. Do not allocate rollback journals, diagnostic collections, or per-item bookkeeping on the happy path just to make rare failures easier to unwind. If the affected work can be enumerated again safely, let the error path recompute it and clean up there. Keep explicit rollback state only when side effects are non-idempotent, external, ordered in a way that cannot be rebuilt, or otherwise impossible to reconstruct.

## Releases

- Use patch releases for small fixes and minor releases for larger user-visible changes.
- Automatic releases batch every 10 unreleased commits; set `.github/release-bump` to `minor` for larger user-visible batches.
- Release notes should list the exact commits since the previous tag.

## Server And Web

- Feed-item endpoints in `internal/web/` must return the enriched shape callers expect: `feed.EnrichFeedItems(...)`, bookmark state, subscribe/follow URLs, and every field the caller reads.
- Do not narrow a shared query for one caller if another caller needs the data. Add a separate query.
- For web UI bugs, inspect the live DOM before source: element HTML, computed visibility, layout box, inline style, and classes.
- After server, web, static, or component changes that affect the running app, run `scripts/dev/build.sh restart`.
- For Go changes, run `go test ./...`.

## Android

- Android must render normal UI state without live Igloo server access.
- Room mirrors the documented server schema; schema bumps need migrations in `IglooMigrations`.
- User state belongs in thin side tables joined at read time.
- Cursors are opaque. Server-owned identifiers stay server-owned.
- Sync must converge for the retention window, associated assets, bookmarks, likes, and their assets. Partial sync is not success.
- Retention widening triggers replay/backfill; narrowing prunes; bookmarks and likes survive prune.
- Use project scripts: `android/build.sh`, `android/test.sh`, `scripts/dev/build.sh android`, `scripts/dev/build.sh all`.
- Before committing Android changes, run the focused `android/test.sh <ClassFilter>` proof for the touched area.
- Before pushing Android changes, run full `android/test.sh` and a separate APK build proof. Use `android/build.sh` when installing/relaunching on the device is appropriate; use `android/build.sh apk` when only APK compilation is needed or when device install is not available. Do not treat `android/test.sh` compilation as a substitute for the build lane unless the user explicitly narrows verification or says they are building it.
- Do not reset Android app data or preferences as a debugging shortcut.
