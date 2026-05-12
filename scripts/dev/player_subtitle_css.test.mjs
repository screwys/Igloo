import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const css = readFileSync(new URL("../../static/style.css", import.meta.url), "utf8");
const playerJs = readFileSync(new URL("../../static/js/src/player/index.js", import.meta.url), "utf8");

function cssVar(name) {
  const match = css.match(new RegExp(`${name}:\\s*(-?[0-9]+)px;`));
  assert.ok(match, `missing ${name}`);
  return Number(match[1]);
}

test("player subtitles use separate offsets for normal and fullscreen states", () => {
  assert.equal(cssVar("--player-subtitles-offset-idle"), 36);
  assert.equal(cssVar("--player-subtitles-offset-controls"), 96);
  assert.equal(cssVar("--player-subtitles-offset-fullscreen-idle"), 52);
  assert.equal(cssVar("--player-subtitles-offset-fullscreen-controls"), 104);
  assert.match(css, /--player-subtitles-font-size-fullscreen:\s*clamp\(28px,\s*2vw,\s*52px\);/);
});

test("fullscreen subtitles keep browser-native cue styling only", () => {
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper #video-player\.video-js \.vjs-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-idle/,
  );
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper video::cue[\s\S]*?--player-subtitles-font-size-fullscreen/,
  );
  assert.doesNotMatch(css, /video::-webkit-media-text-track-display[\s\S]*?transform:/);
});

test("control-visible subtitles use the app-owned controller state", () => {
  assert.ok(cssVar("--player-subtitles-offset-controls") > cssVar("--player-subtitles-offset-idle"));
  assert.ok(cssVar("--player-subtitles-offset-fullscreen-controls") > cssVar("--player-subtitles-offset-fullscreen-idle"));
  assert.match(
    css,
    /\.player-layout:fullscreen \.player-wrapper \.dashboard-media-controller\[data-player-controls-visible="1"\] #video-player\.video-js \.vjs-text-track-display[\s\S]*?--player-subtitles-offset-fullscreen-controls/,
  );
});

test("player controls hide from app-owned visibility state, not lingering focus", () => {
  assert.match(
    css,
    /#main-media-controller\[data-player-controls-ready\]:not\(\[data-player-controls-visible="1"\]\) media-control-bar\.dashboard-media-control-bar[\s\S]*?opacity:\s*0;/,
  );
  assert.doesNotMatch(css, /#main-media-controller\[userinactive\]:not\(:focus-within\)/);
  assert.doesNotMatch(css, /\.player-wrapper:not\(:hover\):not\(:focus-within\) #main-media-controller\[mediapaused\]/);
});

test("player JavaScript converts subtitle pixel offsets to cue line percentages", () => {
  assert.match(playerJs, /function subtitleOffsetPx\(\)/);
  assert.match(playerJs, /function setupPlayerControlsVisibility\(\)/);
  assert.match(playerJs, /data-player-controls-visible/);
  assert.match(playerJs, /--player-subtitles-offset-fullscreen-controls/);
  assert.match(playerJs, /video\.getBoundingClientRect\(\)/);
  assert.match(playerJs, /100 - \(baseOffset \/ height \* 100\)/);
  assert.doesNotMatch(playerJs, /cue\.line = liftUp \? 88 : 96/);
});
