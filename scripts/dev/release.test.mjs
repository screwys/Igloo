import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  bumpSemver,
  normalizeReleaseBump,
  parseAndroidVersion,
  renderReleaseNotes,
  updateAndroidBuildGradle,
} from "./release.mjs";

test("bumps patch, minor, and major versions", () => {
  assert.equal(bumpSemver("1.0.0", "patch"), "1.0.1");
  assert.equal(bumpSemver("1.0.9", "minor"), "1.1.0");
  assert.equal(bumpSemver("1.9.9", "major"), "2.0.0");
});

test("rejects unsupported version bumps", () => {
  assert.throws(() => bumpSemver("1.0.0", "weird"), /unsupported bump/);
  assert.throws(() => bumpSemver("1.0", "patch"), /invalid semver/);
});

test("normalizes release bump inputs", () => {
  assert.equal(normalizeReleaseBump(" minor\n"), "minor");
  assert.equal(normalizeReleaseBump(" major\n"), "major");
  assert.throws(() => normalizeReleaseBump("weird"), /unsupported bump/);
});

test("release workflow is manually dispatched", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /\n  workflow_dispatch:\n/);
  assert.doesNotMatch(workflow, /\n  push:\n/);
  assert.doesNotMatch(workflow, /prepare-auto/);
  assert.doesNotMatch(workflow, /threshold 30/);
});

test("release workflow dispatches CodeQL for release tags", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );

  assert.match(
    workflow,
    /gh workflow run codeql\.yml --ref "\$\{\{ steps\.release\.outputs\.tag \}\}"/,
  );
});

test("release workflow allows manual major releases", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /\n          - major\n/);
  assert.match(workflow, /--description-file \.github\/release-description\.md/);
});

test("release workflow signs release commits and tags", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /RELEASE_GPG_PRIVATE_KEY/);
  assert.match(workflow, /RELEASE_GPG_PASSPHRASE/);
  assert.match(workflow, /git commit -S -m "release \$\{\{ steps\.release\.outputs\.version \}\}"/);
  assert.match(workflow, /git tag -s "\$\{\{ steps\.release\.outputs\.tag \}\}"/);
  assert.match(workflow, /git add android\/app\/build\.gradle\.kts \.github\/release-description\.md/);
  assert.doesNotMatch(workflow, /\.github\/release-bump/);
  assert.doesNotMatch(workflow, /package\.json/);
  assert.doesNotMatch(workflow, /package-lock\.json/);
  assert.doesNotMatch(workflow, /git tag -a "\$\{\{ steps\.release\.outputs\.tag \}\}"/);
});

test("container release publishes signed provenance attestation", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/container-release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /\n  id-token: write\n/);
  assert.match(workflow, /\n  attestations: write\n/);
  assert.match(workflow, /\n        id: build\n/);
  assert.match(workflow, /actions\/setup-go@v6/);
  assert.match(workflow, /DeterminateSystems\/determinate-nix-action@v3/);
  assert.match(workflow, /go test \.\/\.\.\./);
  assert.match(workflow, /nix build \.#container --print-build-logs/);
  assert.match(workflow, /docker load < result/);
  assert.match(workflow, /SOURCE_IMAGE: ghcr\.io\/screwys\/igloo:latest/);
  assert.match(workflow, /docker tag "\$SOURCE_IMAGE" "\$tag"/);
  assert.match(workflow, /docker push "\$tag"/);
  assert.match(workflow, /uses: actions\/attest@v4/);
  assert.match(workflow, /subject-name: ghcr\.io\/\$\{\{ github\.repository_owner \}\}\/igloo/);
  assert.match(workflow, /subject-digest: \$\{\{ steps\.build\.outputs\.digest \}\}/);
  assert.match(workflow, /push-to-registry: true/);
  assert.doesNotMatch(workflow, /docker\/build-push-action/);
});

test("container images run as non-root by default", () => {
  const dockerfile = readFileSync(new URL("../../Dockerfile", import.meta.url), "utf8");
  const flake = readFileSync(new URL("../../flake.nix", import.meta.url), "utf8");
  const compose = readFileSync(new URL("../../compose.yaml", import.meta.url), "utf8");

  assert.match(dockerfile, /^USER 10001:10001$/m);
  assert.match(dockerfile, /HOME=\/tmp/);
  assert.match(dockerfile, /chown -R 10001:10001 \/igloo/);
  assert.match(flake, /User = "10001:10001";/);
  assert.match(flake, /"HOME=\/tmp"/);
  assert.match(flake, /chown -R 10001:10001 igloo/);
  assert.match(compose, /user: "\$\{IGLOO_UID:-1000\}:\$\{IGLOO_GID:-1000\}"/);
});

test("Android release publishes signed provenance attestation for the APK", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/android-release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /\n  id-token: write\n/);
  assert.match(workflow, /\n  attestations: write\n/);
  assert.match(workflow, /uses: actions\/attest@v4/);
  assert.match(workflow, /subject-path: release-artifacts\/\*\.apk/);
});

test("CodeQL runs on code changes and published releases instead of a weekly schedule", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/codeql.yml", import.meta.url),
    "utf8",
  );

  assert.match(
    workflow,
    /\n  push:\n    paths-ignore:\n      - "\*\*\/\*\.md"\n/,
  );
  assert.match(
    workflow,
    /\n  pull_request:\n    paths-ignore:\n      - "\*\*\/\*\.md"\n/,
  );
  assert.match(
    workflow,
    /\n  release:\n    types:\n      - published\n/,
  );
  assert.doesNotMatch(workflow, /\n  schedule:\n/);
  assert.doesNotMatch(workflow, /cron:/);
});

test("CI workflows ignore Markdown-only pushes and pull requests", () => {
  for (const workflowName of ["android-ci.yml", "go-ci.yml", "semgrep.yml", "codeql.yml"]) {
    const workflow = readFileSync(
      new URL(`../../.github/workflows/${workflowName}`, import.meta.url),
      "utf8",
    );

    assert.match(
      workflow,
      /\n  push:\n    paths-ignore:\n      - "\*\*\/\*\.md"\n/,
      `${workflowName} push trigger should ignore Markdown-only changes`,
    );
    assert.match(
      workflow,
      /\n  pull_request:\n    paths-ignore:\n      - "\*\*\/\*\.md"\n/,
      `${workflowName} pull_request trigger should ignore Markdown-only changes`,
    );
  }
});

test("updates android release version fields", () => {
  const source = `
android {
    defaultConfig {
        versionCode = 3
        versionName = "1.0.0"
    }
}
`;
  const updated = updateAndroidBuildGradle(source, "1.0.1", 4);
  assert.match(updated, /versionCode = 4/);
  assert.match(updated, /versionName = "1.0.1"/);
});

test("reads android release version fields", () => {
  const parsed = parseAndroidVersion(`
android {
    defaultConfig {
        versionCode = 3
        versionName = "1.0.0"
    }
}
`);
  assert.deepEqual(parsed, {
    versionCode: 3,
    versionName: "1.0.0",
  });
});

test("renders exact commit release notes", () => {
  const notes = renderReleaseNotes({
    newTag: "v1.0.1",
    previousTag: "v1.0.0",
    repository: "screwys/Igloo",
    commits: [
      {
        sha: "1111111111111111111111111111111111111111",
        subject: "fixed hover not being triggered in feed",
      },
      {
        sha: "2222222222222222222222222222222222222222",
        subject: "added a release helper",
      },
    ],
  });

  assert.match(notes, /^## Release v1\.0\.1/m);
  assert.match(notes, /^changes:$/m);
  assert.match(
    notes,
    /- fixed hover not being triggered in feed \(\[1111111\]\(https:\/\/github\.com\/screwys\/Igloo\/commit\/1111111111111111111111111111111111111111\)\)/,
  );
  assert.doesNotMatch(notes, /^## commits/m);
  assert.doesNotMatch(notes, /since `v1\.0\.0`/);
});

test("renders release description before commit list", () => {
  const notes = renderReleaseNotes({
    newTag: "v2.0.0",
    repository: "screwys/Igloo",
    description: "Igloo no longer depends on RSSHub.",
    commits: [
      {
        sha: "1111111111111111111111111111111111111111",
        subject: "replace rsshub ingest",
      },
    ],
  });

  assert.match(notes, /^## Release v2\.0\.0\n\nIgloo no longer depends on RSSHub\.\n\nchanges:/m);
});
