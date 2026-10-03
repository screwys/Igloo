---
name: igloo-android-sync
description: Use when changing Igloo Android sync, Room schemas, retention, importers, cursors, asset convergence, or offline data.
---

# Igloo Android sync

Trace the affected data from server storage through the sync API, importer, Room, and UI reader. Keep normal rendering available offline. Follow AGENTS.md for released-client compatibility and build/device rules.

- Treat server identifiers, cursors, hashes, asset IDs, and generations as opaque inputs.
- Mirror the server wire contract. Store complete owner payloads and preserve unknown fields. Keep typed storage columns for local joins, filters, and ordering; presentation fields stay in decoded records.
- Normalize known fields and compute content classification at capture. Preserve existing values during migration. Update `StoredContent`, `IglooMigrations`, generated schemas, and affected contract fixtures when physical storage changes.
- Keep user state in thin side tables joined at read time.
- Sync until completion, cancellation, or a real failure. Include the retention window, saved content, required context and identities, and associated assets.
- Widening retention requires replay or backfill. Narrowing prunes content while preserving bookmarks, likes, and their assets.
- Preserve the current item and user progress while mirrored data changes.

Use `just check-schema` for schema/contract changes and focused existing checks such as `just test-android <ClassFilter>` or `just test-go-package <package>`. Android-owned changes require the final `just build-android` proof described in AGENTS.md.
