import assert from "node:assert/strict";
import test from "node:test";

import {
  bumpSemver,
  renderReleaseNotes,
  updateAndroidBuildGradle,
  updatePackageJsonText,
  updatePackageLockText,
} from "./release.mjs";

test("bumps patch and minor versions", () => {
  assert.equal(bumpSemver("1.0.0", "patch"), "1.0.1");
  assert.equal(bumpSemver("1.0.9", "minor"), "1.1.0");
});

test("rejects unsupported version bumps", () => {
  assert.throws(() => bumpSemver("1.0.0", "major"), /unsupported bump/);
  assert.throws(() => bumpSemver("1.0", "patch"), /invalid semver/);
});

test("updates package metadata versions", () => {
  const packageJson = updatePackageJsonText(
    JSON.stringify({ name: "igloo", version: "1.0.0" }, null, 2) + "\n",
    "1.0.1",
  );
  assert.equal(JSON.parse(packageJson).version, "1.0.1");
  assert.match(packageJson, /\n$/);

  const lockJson = updatePackageLockText(
    JSON.stringify({
      name: "igloo",
      version: "1.0.0",
      packages: {
        "": { name: "igloo", version: "1.0.0" },
      },
    }),
    "1.0.1",
  );
  const parsed = JSON.parse(lockJson);
  assert.equal(parsed.version, "1.0.1");
  assert.equal(parsed.packages[""].version, "1.0.1");
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
