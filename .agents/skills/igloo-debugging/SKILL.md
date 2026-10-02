---
name: igloo-debugging
description: Use when investigating Igloo failures, regressions, CI problems, missing data, or generated-output drift.
---

# Igloo debugging

Follow the reported failure through current source and local evidence. Establish which component owns the failing behavior, fix the cause, and verify that path. Keep confirmed facts separate from hypotheses. Use AGENTS.md for evidence, privacy, commands, and completion rules.

## Identity and media

For missing profiles or assets, establish when content entered storage, which author, quote, mention, coauthor, or source identity should have seeded `channel_profiles`, and when `fetched_at` and cached files became ready. Trace the ingest, seed, profile worker, or retry responsible for preparing them.

Choose a presentation fix when the data and file already existed before render. Otherwise correct the pipeline. Preserve supported empty-profile behavior while metadata is fetched. Keep data repair separate from the production fix.

## CI and Android

Read the actual branch run and first relevant failure. CI-only timing failures call for evidence about scheduling or the test environment before changing product behavior. For hangs, inspect worker output or thread stacks.

Android CI uses `android/test.sh`. Use named `just` recipes locally, or the exact CI Gradle lane when reproducing it. A cold-run reproduction may need `--rerun-tasks --no-daemon`. Trace Room migrations and asset convergence while preserving app data and preferences.

## Generated output

Use the repository generator and checker for drift. Regenerate stale output before judging source behavior. Keep localization scoped to the requested work.

Follow AGENTS.md for data contracts. Use `igloo-android-sync` or `igloo-web-ui-guidance` for work in those areas.
