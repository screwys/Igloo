---
name: igloo-web-ui-guidance
description: Use when designing, implementing, reviewing, or debugging Igloo web layouts, controls, CSS, templates, or browser interactions.
---

# Igloo web UI guidance

Follow AGENTS.md for design, browser use, privacy, and verification. Start from the reported control and the existing Igloo equivalent.

## Layout and controls

Trace the relevant template and CSS together. Distinguish visual scale, alignment, spacing, and clipping before choosing a change. Account for the available container width and neighboring controls.

Compare visible artwork and text when judging alignment. Equal element boxes or SVG dimensions can contain different visual sizes. Preserve icon proportions and use the existing icon family.

Keep hover geometry and foreground/accent colors stable. Use accent color for selection without added rings, glows, or filled containers. Retain keyboard focus indicators. Match feedback to the control's shape.

## Rendering and interaction

For absent content, trace the handler, enrichment, template, generated output, and JavaScript caller. For present but hidden content, trace the CSS cascade, responsive rules, and runtime classes. For wrong data, follow the data contracts in AGENTS.md and use `igloo-debugging` when investigation is needed.

Keep component behavior and resource lifetime with the code that owns the component.

Use standard browser APIs and CSS for Firefox and Chromium. Put vendor-specific selectors in separate rules so an unsupported selector cannot invalidate a standard rule.

Choose runtime inspection to answer the unresolved layout or event question. Report observed behavior separately from source and build checks.
