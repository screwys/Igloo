---
name: igloo-web-ui-debugging
description: Use when changing or debugging Igloo web UI, static assets, templ components, feed/player/story interactions, hover cards, subtitles, CSS visibility/layout, browser behavior, or server-rendered UI state.
---

# Igloo Web UI Debugging

## Cross-Browser Invariant

Never assume Chromium. Igloo web behavior must work in Firefox and Chromium. Start with standard APIs and CSS, then isolate any vendor-specific fallback. Do not combine a vendor-only selector with standard selectors in one comma-separated rule: browsers use unforgiving selector-list parsing and may discard the entire rule when one selector is unsupported. A pass in one engine is evidence only for that engine; when the reported browser is known, reason from and verify that engine's behavior.

Inspect the running UI before editing source when a UI symptom is visible or reproducible.

## Flow

1. Identify whether the problem is absence, hidden content, wrong data, wrong layout, stale generated output, or client-side mutation.
2. Inspect the live DOM when possible: element HTML, computed visibility, display, opacity, layout box, classes, inline styles, event handlers, and console errors.
   For icon-and-label alignment, DOM boxes are only structural evidence. Compare the rendered icon height with the visible font ink or cap height in a screenshot before deciding the problem is positional. Equal box centers can still look wrong when a 20px icon is paired with roughly 11px-tall text; a shared top edge often exposes a scale mismatch, not an offset bug.
3. If the element is absent, trace the render path through handler, enrichment, templ component, generated output, and JavaScript caller.
4. If the element is present but wrong or hidden, inspect CSS cascade, responsive rules, container dimensions, runtime classes, and media query behavior before changing markup.
5. For feed item surfaces, keep handler responses enriched with bookmark state, follow or subscription URLs, platform/media metadata, and every field the caller reads.
6. For avatars, banners, names, bios, or hover cards, separate presentation bugs from readiness bugs. Patch the UI only when the DB row and cached file already existed before render.
7. Regenerate templ/static assets through `just check-drift` instead of editing generated files directly.

## Icon And Text Geometry

- Diagnose size before position. Inspect isolated icon/label pairs and compare visible ink bounds, not only CSS width, line-height, or flex-item rectangles.
- Identical SVG `width`, `height`, and `viewBox` values do not imply equal icon size: paths can occupy very different fractions of the canvas. Measure the rendered artwork bounds for every icon in the set. Preserve each icon's intended proportions; never stretch or redraw its path merely to match a target bound. Prefer a better-fitting icon designed in the same visual family, or adjust the shared icon slot and font relationship when the whole set needs scaling.
- Distinguish three separate failures: mismatched visual scale, unequal center alignment, and inconsistent row spacing. Do not use a spacing or offset change to compensate for a scale mismatch.
- When icons visibly dominate the font, first adjust the icon-to-font size ratio while preserving ordinary flex centering. Use transforms, relative offsets, margins, or artificial line boxes only when evidence shows a remaining positional mismatch after sizing is correct.
- Verify both normal and active rows because font weight changes the label's visible bounds. Reinspect the rendered result at the reported viewport; identical DOM centers alone do not establish visual consistency.

## Common Surfaces

- Feed cards, conversation threads, loaded thread fragments, hover cards, source badges, story tray, moments, fullscreen player, subtitle overlays, and media readiness indicators.
- Server handlers and templates under `internal/web` and `internal/components`.
- Browser JavaScript and CSS under `static`.

## Verification

- After server, web, static, or component changes that affect the running app, run `just restart`.
- For Go handler or template behavior, run focused Go tests and `just test-go` when practical.
- For generated catalog or templ drift, run `just i18n-check` or `just check-drift` and inspect the resulting diff.
- For visual or interaction fixes, give the user the relevant viewport and state to confirm; do not claim visual confirmation yourself.

Useful commands:

```bash
just restart
just test-go-package ./internal/web
just test-go-package ./internal/components
just i18n-check
```
