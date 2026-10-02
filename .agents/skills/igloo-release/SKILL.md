---
name: igloo-release
description: Use when preparing or publishing an Igloo release, changing release workflows, or recovering a partial release.
---

# Igloo release

Follow AGENTS.md for publishing authorization and verification. Obtain both the requested bump and exact user-written summary, finish product changes, and start with a clean working tree.

Use `just release <patch|minor|major> "<user summary>"` to publish, or `just release-local ...` for an explicitly local signed tag. The recipe owns metadata, the signed commit/tag, generated notes, and publishing. Notes put the user's summary first, followed by the exact commits since the previous tag.

## Signing and artifacts

Release signing uses `RELEASE_GPG_PRIVATE_KEY` and `RELEASE_GPG_PASSPHRASE`. Optional `RELEASE_GIT_USER_NAME` and `RELEASE_GIT_USER_EMAIL` set the commit identity. Keep secret values private.

Artifact workflows verify tags against `.github/release-gpg.pub` before publication. APKs and containers carry GitHub attestations, and containers use keyless cosign signatures.

## Partial-release recovery

Inspect the existing commit, tag, GitHub Release, and workflow runs before retrying. Use `.github/scripts/create-release-tag.sh` and `scripts/dev/release.mjs` as the source of the release sequence.

When building notes from a signed tag, use `git for-each-ref` with `%(contents:subject)` and `%(contents:body)`. Full tag contents include the PGP signature. Supply notes to `gh` with `--notes-file`.

Check the final release body and dispatched `container-release.yml`, `android-release.yml`, and `windows-release.yml` runs. Report the release URL, workflow status, and remaining failures.
