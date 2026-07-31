import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const escapeRegExp = (value) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

test("runtime downloader tool versions come from shared requirements", () => {
  const requirements = readFileSync(
    new URL("../../requirements-runtime.txt", import.meta.url),
    "utf8",
  );
  const dockerfile = readFileSync(new URL("../../Dockerfile", import.meta.url), "utf8");
  const flake = readFileSync(new URL("../../flake.nix", import.meta.url), "utf8");
  const renovate = JSON.parse(
    readFileSync(new URL("../../renovate.json", import.meta.url), "utf8"),
  );
  const runtimeVersion = (packageName) => {
    const version = requirements.match(
      new RegExp(`^${escapeRegExp(packageName)}==([^\\s]+)$`, "m"),
    )?.[1];
    assert.ok(version, `${packageName} must be pinned in requirements-runtime.txt`);
    return version;
  };
  const runtimeToolMetadata = (packageName, pypiName) => {
    const match = requirements.match(
      new RegExp(
        `^# renovate: packageName=${escapeRegExp(
          packageName,
        )} pypiName=${escapeRegExp(
          pypiName,
        )} versioning=pep440 sha256=([a-f0-9]{64})$`,
        "m",
      ),
    );
    assert.ok(match, `${packageName} must carry its Nix source hash beside its version`);
    return match[1];
  };

  assert.match(requirements, /^yt-dlp==[^\s]+$/m);
  assert.match(requirements, /^gallery-dl==[^\s]+$/m);
  assert.match(dockerfile, /COPY requirements-runtime\.txt \/tmp\/requirements-runtime\.txt/);
  assert.match(dockerfile, /pip install --no-cache-dir -r \/tmp\/requirements-runtime\.txt/);
  assert.doesNotMatch(dockerfile, /ARG YT_DLP_VERSION|ARG GALLERY_DL_VERSION/);
  assert.doesNotMatch(dockerfile, /yt-dlp==|gallery-dl==/);
  assert.match(flake, /runtimeToolVersion "yt-dlp"/);
  assert.match(flake, /runtimeToolVersion "gallery-dl"/);
  runtimeToolMetadata("yt-dlp", "yt_dlp");
  runtimeToolMetadata("gallery-dl", "gallery_dl");
  assert.match(flake, /builtins\.readFile \.\/requirements-runtime\.txt/);
  assert.match(flake, /runtimeTools = lib\.genAttrs/);
  assert.doesNotMatch(flake, /version = "2026\.|sha256 = "[a-f0-9]{64}"/);
  assert.doesNotMatch(flake, /pname = "yt-dlp";\n\s+version = "/);
  assert.doesNotMatch(flake, /pname = "gallery_dl";\n\s+version = "/);

  assert.ok(!renovate.enabledManagers.includes("pip_requirements"));
  const runtimeManager = renovate.customManagers.find((manager) =>
    manager.managerFilePatterns?.includes("/(^|/)requirements-runtime\\.txt$/"),
  );
  assert.ok(runtimeManager, "Renovate must update the shared runtime requirements file");
  assert.match(runtimeManager.matchStrings[0], /currentDigest/);
  assert.match(runtimeManager.autoReplaceStringTemplate, /newDigest/);
  assert.match(runtimeManager.autoReplaceStringTemplate, /newValue/);
  const managedRuntimeTools = [
    ...requirements.matchAll(new RegExp(runtimeManager.matchStrings[0], "g")),
  ];
  assert.deepEqual(
    managedRuntimeTools.map(({ groups }) => ({
      name: groups.depName,
      version: groups.currentValue,
      sha256: groups.currentDigest,
    })),
    [
      {
        name: "yt-dlp",
        version: runtimeVersion("yt-dlp"),
        sha256: runtimeToolMetadata("yt-dlp", "yt_dlp"),
      },
      {
        name: "gallery-dl",
        version: runtimeVersion("gallery-dl"),
        sha256: runtimeToolMetadata("gallery-dl", "gallery_dl"),
      },
    ],
  );
});
