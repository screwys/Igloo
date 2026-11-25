import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("runtime downloaders follow upstream HEAD", () => {
  const requirements = readFileSync(new URL("../../requirements-runtime.txt", import.meta.url), "utf8");
  const dockerfile = readFileSync(new URL("../../Dockerfile", import.meta.url), "utf8");
  const sourceLines = requirements.split("\n").filter((line) => !line.startsWith("#") && line.includes(" @ "));
  const sources = Object.fromEntries(sourceLines.map((line) => line.split(" @ ")));

  assert.deepEqual(sources, {
    "yt-dlp": "https://github.com/yt-dlp/yt-dlp/archive/master.tar.gz",
    "gallery-dl": "https://codeberg.org/mikf/gallery-dl/archive/master.tar.gz",
  });
  for (const [name, url] of Object.entries(sources)) {
    assert.ok(dockerfile.includes(`ADD ${url} /tmp/${name}.tar.gz`));
  }
  assert.ok(dockerfile.includes("pip install --no-cache-dir /tmp/yt-dlp.tar.gz /tmp/gallery-dl.tar.gz"));
});
