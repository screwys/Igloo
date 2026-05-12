import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const css = readFileSync(new URL("../../static/style.css", import.meta.url), "utf8");

function cssVar(name) {
  const match = css.match(new RegExp(`${name}:\\s*([0-9]+)px;`));
  assert.ok(match, `missing ${name}`);
  return Number(match[1]);
}

test("player subtitles use separate offsets for normal and fullscreen states", () => {
  assert.equal(cssVar("--player-subtitles-offset-idle"), 10);
  assert.equal(cssVar("--player-subtitles-offset-controls"), 26);
  assert.equal(cssVar("--player-subtitles-offset-fullscreen-idle"), 42);
  assert.equal(cssVar("--player-subtitles-offset-fullscreen-controls"), 48);
});

test("fullscreen subtitles stay inside the video when controls are hidden", () => {
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper video::-webkit-media-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-idle/,
  );
  assert.match(
    css,
    /\.player-layout:-webkit-full-screen \.player-wrapper video::-webkit-media-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-idle/,
  );
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper #video-player\.video-js \.vjs-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-idle/,
  );
});

test("control-visible subtitles use the lower control offsets", () => {
  assert.ok(cssVar("--player-subtitles-offset-controls") < 68);
  assert.ok(cssVar("--player-subtitles-offset-fullscreen-controls") < 54);
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper \.dashboard-media-controller:hover video::-webkit-media-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-controls/,
  );
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper \.dashboard-media-controller:hover #video-player\.video-js \.vjs-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-controls/,
  );
});
