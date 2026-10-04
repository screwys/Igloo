# Igloo agent guide

## Project

Igloo is a Go/PostgreSQL server with web and Android clients. Android uses Room/SQLite. `android/` is the current Android app. Unqualified "mobile" means Android. Web behavior must work in Firefox and Chromium using standard APIs and CSS.

Runtime defaults are `~/.local/share/igloo/` and `~/.config/igloo/` on the host, `/igloo/data` and `/igloo/config` in containers, and `/app/static` for bundled assets.

## Development

- Build the behavior the user needs with as little machinery as possible. Add controls, states, fallbacks, and dependencies when they serve that outcome.
- Preserve supported behavior, stored data, and client contracts. Base changes and restrictions on established requirements and demonstrated failures.
- Fix the responsible code with the simplest complete change. Prefer correcting or removing a mechanism over adding recovery layers around it. Keep unrelated cleanup and speculative hardening outside the task.
- Judge optimizations across supported platform mixes, archive sizes, and retention settings. Treat local measurements as one workload sample.
- Preserve local state until network operations succeed. Keep one-time repairs separate from normal startup.
- In Go, keep the success path lean. Recompute cleanup work on failure when safe, retaining rollback state when side effects cannot be reconstructed.
- Use plain English and generic names in comments, examples, and commits. Keep real handles, IDs, and private runtime values out of tracked files.

## Upstream tools

Let yt-dlp and gallery-dl own platform extraction. Use their documented CLI and output formats, with rolling HEAD/nightly builds across host and packaged environments.

Keep Igloo's integration thin and compatible with upstream updates. Use upstream's default extractor behavior. Configure documented integration options such as output paths, cookies, and format selection for established Igloo requirements. Assess new features against what upstream supplies before taking on custom extraction, private API parsing, or extractor/client overrides.

Prove new yt-dlp and gallery-dl flags, arguments, endpoints, and output formats through Igloo before building features around them. Confirm the requested result, including real playback when relevant, and report cookie requirements or failures upfront.

## Design

- Reuse established controls and layouts, adapting them to each client's available space. Keep controls readable, visually consistent, and easy to use.
- Use Igloo's existing icon buttons and icon set (Material) for actions. Reuse their size and styling, with tooltip and accessibility labels, instead of adding large text buttons.
- Before adding a control, inspect and reuse its closest existing template, CSS, JavaScript, and action handler. Use Igloo's dropdowns instead of native selects. For a routine menu item using established controls and icons, source inspection and relevant existing checks are sufficient. Check rendering or interaction in the browser only when a specific unresolved runtime question could change the implementation.
- Use short functional labels and one clear control for each action. Add a short helper line only when the user would otherwise be stuck.
- Judge interface changes by how they affect the user's activity and existing workflow. Prefer fewer coherent interactions over extra modes, decorations, and explanations.
- Confirm destructive actions with an Igloo modal on web or Compose `AlertDialog` on Android.

## Evidence

Start from the user's report and relevant source. Trust their observations and corrections, revisiting ruled-out explanations only with new evidence. Follow the reported failure through its responsible path. An unsuccessful reproduction or passing unrelated check leaves that investigation open.

Use local rows, files, and logs for data questions, preferring read-only Igloo MCP tools when available. The server uses PostgreSQL; maintenance tools attach through the state directory or `IGLOO_DATABASE_URL`. SQLite files are legacy archives. Use stored identifiers and data before fetching public platform pages.

Use the browser for a specific unresolved runtime question that could change the fix. State that question before opening a browser. Use `https://localhost:8443` for all local browser checks, including playback investigations. Reuse the existing session and target. Do not create temporary browser fixtures, servers, or new ports. Do not change ports or open more sessions to retry an approval. Basic edits can be completed from the report and source. Check private material only for existence, masking values as `***` if a format check is necessary. Do not capture private data or screenshots.

Never launch fullscreen. Keep UI verification in the background and do not take over the user's desktop.

## Commands and verification

Use `just` from the repository root for routine builds, checks, generators, and releases. Bare `just` lists recipes and side effects. Raw commands are appropriate for read-only evidence, installer bootstrap, exact CI reproduction, partial-release recovery, or a narrow proof with no recipe.

- Write tests only when requested. Run relevant existing checks. Use focused recipes during development, `just test` for the proportional gate, and `just test-full` when the full suite is needed. Inspect skips and ignored errors.
- Keep verification proportional to the behavior changed. Reuse completed check results. Repeat a check only after a relevant change or failure. Do not build temporary test harnesses for routine controls whose behavior can be traced through existing code and checks.
- Rely on `.githooks/pre-push` for push checks without repeating them manually. Treat new or high-signal production `errcheck` findings as blockers and explain existing findings left unresolved.
- Ordinary push checks select affected Go packages and callers and reuse Go and Gradle caches. Use `IGLOO_PRE_PUSH_FULL=1 git push` for an explicit full cold check.
- Regenerate templ and bundled assets with `just check-drift`. Update localization with `just i18n-sync` and verify it with `just i18n-check`.
- After server, web, static, or component changes that affect the running app, run `just restart`.
- After Android app source, resources, manifest, Gradle configuration, or Room schema changes, run `just build-android` before finishing or committing. It builds, installs, and relaunches the app. JVM tests and compilation alone do not replace it. Report unavailable tooling or devices explicitly.
- Host-only Android scripts need shell validation and the relevant script proof. Generated `android/app/src/main/res/values/strings.xml` changes solely from shared localization need localization and proportional checks, without Android JVM tests or device installation. Other Android resources use the normal Android gate.
- Treat Android JVM final-field mutation warnings as failures. Use fakes or interfaces instead of concrete-class mocks that cause them.
- Keep automated Android device interaction to installation and relaunch unless the user explicitly requests more. Source and build evidence establish implementation, not live appearance or playback. Request user verification for a concrete unresolved question.

## Data contracts

- PostgreSQL schema and triggers live in `internal/db/postgres/migrations/` and use Goose. Add a new migration for changes to an applied schema. Fixed sqlc queries live in `internal/db/queries/`; regenerate their bindings with `just check-drift`. The SQLite server schema supports legacy archive conversion.
- Android stores complete synced owner payloads with typed columns for joins, filters, and ordering. Preserve unknown payload fields and normalize known fields at capture. Presentation fields belong in decoded records; changes to stored query columns require a Room migration. Compute content classification during capture and migration.
- Trace changed fields and queries through every affected caller. Compare returned data and state transitions when changing shared queries.
- Server storage owns channel and media identity, assets, and cursors. Prepare identity and media during capture and ingest. Preserve profile navigation and following when metadata is empty.
- Feed-item responses need `feed.EnrichFeedItems(...)`, bookmark state, follow or subscription URLs, and every field the caller reads. Give callers separate queries when their data needs differ.
- Likes and bookmarks must persist the content the user saw, including body, identity, media, and files. Trace sparse action payloads as capture problems.
- Preserve saved content and its dependencies across pruning and restoration. Keep user state separate from mirrored content.

The server published as `latest` must remain compatible with the latest released APK, even when development is ahead. Verify the shipped client's request and wire contract when changing sync materialization or publishing `latest`. Current Android CI and schema version numbers do not establish released-client compatibility.

Use `igloo-android-sync` for the Android mirror, `igloo-debugging` for investigations, and `igloo-web-ui-guidance` for web UI work.

The web tests reuse one database and reset its data between fixtures. Keep tests using `newTestServer` sequential. Use a separate database for schema deletion or process lifecycle checks.

## Git and releases

- For CI fixes, commit and push the verified fix unless the user says otherwise or publishing is blocked. Report the exact blocker.
- Releases require both the user's bump and exact summary. Use `just release <patch|minor|major> "<user summary>"` to publish or `just release-local ...` for a local signed tag.
