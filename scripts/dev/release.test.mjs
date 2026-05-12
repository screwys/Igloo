import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  bumpSemver,
  normalizeReleaseBump,
  parseAndroidVersion,
  planAutomaticRelease,
  renderReleaseNotes,
  updateAndroidBuildGradle,
  updateReleaseBumpText,
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

test("normalizes tracked release bump state", () => {
  assert.equal(normalizeReleaseBump(" minor\n"), "minor");
  assert.equal(normalizeReleaseBump(" major\n"), "major");
  assert.equal(updateReleaseBumpText("patch"), "patch\n");
  assert.throws(() => normalizeReleaseBump("weird"), /unsupported bump/);
});

test("plans automatic releases every 30 commits", () => {
  const commits = Array.from({ length: 29 }, (_, index) => ({
    sha: `${index}`.repeat(40).slice(0, 40),
    subject: `change ${index}`,
    body: "",
  }));

  assert.deepEqual(planAutomaticRelease(commits, 30), {
    shouldRelease: false,
    bump: "patch",
    commitCount: 29,
  });

  commits.push({
    sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    subject: "change 29",
    body: "",
  });

  assert.deepEqual(planAutomaticRelease(commits, 30), {
    shouldRelease: true,
    bump: "patch",
    commitCount: 30,
  });
});

test("release workflow uses the 30 commit automatic threshold", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  );

  assert.match(workflow, /prepare-auto --threshold 30\b/);
  assert.doesNotMatch(workflow, /prepare-auto --threshold 20\b/);
  assert.doesNotMatch(workflow, /prepare-auto --threshold 10\b/);
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
  assert.match(workflow, /git add android\/app\/build\.gradle\.kts \.github\/release-bump \.github\/release-description\.md/);
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
  assert.match(workflow, /uses: actions\/attest@v4/);
  assert.match(workflow, /subject-name: ghcr\.io\/\$\{\{ github\.repository_owner \}\}\/igloo/);
  assert.match(workflow, /subject-digest: \$\{\{ steps\.build\.outputs\.digest \}\}/);
  assert.match(workflow, /push-to-registry: true/);
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

test("CodeQL runs on published releases instead of a weekly schedule", () => {
  const workflow = readFileSync(
    new URL("../../.github/workflows/codeql.yml", import.meta.url),
    "utf8",
  );

  assert.match(
    workflow,
    /\n  release:\n    types:\n      - published\n/,
  );
  assert.doesNotMatch(workflow, /\n  push:\n/);
  assert.doesNotMatch(workflow, /\n  pull_request:\n/);
  assert.doesNotMatch(workflow, /\n  schedule:\n/);
  assert.doesNotMatch(workflow, /cron:/);
});

test("plans automatic minor releases from explicit bump state", () => {
  const commits = Array.from({ length: 10 }, (_, index) => ({
    sha: `${index}`.repeat(40).slice(0, 40),
    subject: `change ${index}`,
    body: "",
  }));

  assert.deepEqual(planAutomaticRelease(commits, 10, "minor"), {
    shouldRelease: true,
    bump: "minor",
    commitCount: 10,
  });
});

test("plans automatic major releases from explicit bump state", () => {
  const commits = Array.from({ length: 30 }, (_, index) => ({
    sha: `${index}`.repeat(40).slice(0, 40),
    subject: `change ${index}`,
    body: "",
  }));

  assert.deepEqual(planAutomaticRelease(commits, 30, "major"), {
    shouldRelease: true,
    bump: "major",
    commitCount: 30,
  });
});

test("does not use commit messages as release bump markers", () => {
  const commits = Array.from({ length: 10 }, (_, index) => ({
    sha: `${index}`.repeat(40).slice(0, 40),
    subject: index === 4 ? "release: minor" : `change ${index}`,
    body: index === 5 ? "release: minor" : "",
  }));

  assert.deepEqual(planAutomaticRelease(commits, 10, "patch"), {
    shouldRelease: true,
    bump: "patch",
    commitCount: 10,
  });
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
