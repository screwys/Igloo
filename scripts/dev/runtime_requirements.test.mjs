import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("runtime downloaders follow upstream HEAD", () => {
  const requirements = readFileSync(new URL("../../requirements-runtime.txt", import.meta.url), "utf8");
  const dockerfile = readFileSync(new URL("../../Dockerfile", import.meta.url), "utf8");
  const flake = readFileSync(new URL("../../flake.nix", import.meta.url), "utf8");
  const ci = readFileSync(new URL("../../.github/workflows/ci.yml", import.meta.url), "utf8");
  const release = readFileSync(new URL("../../.github/workflows/container-release.yml", import.meta.url), "utf8");
  const sourceLines = requirements.split("\n").filter((line) => !line.startsWith("#") && line.includes(" @ "));
  const sources = Object.fromEntries(sourceLines.map((line) => line.split(" @ ")));

  assert.deepEqual(sources, {
    "yt-dlp": "https://github.com/yt-dlp/yt-dlp/archive/master.tar.gz",
    "gallery-dl": "https://codeberg.org/mikf/gallery-dl/archive/master.tar.gz",
  });
  for (const [name, url] of Object.entries(sources)) {
    assert.ok(dockerfile.includes(`ADD ${url} /tmp/${name}.tar.gz`));
    assert.ok(flake.includes(`src = runtimeToolSource "${name}";`));
  }
  assert.ok(dockerfile.includes("pip install --no-cache-dir /tmp/yt-dlp.tar.gz /tmp/gallery-dl.tar.gz"));
  assert.ok(ci.includes("nix build --impure --refresh"));
  assert.ok(release.includes("nix build --impure --refresh"));
});
