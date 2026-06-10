---
name: figma-frontend-works
description: Use this skill for high-fidelity Figma-to-frontend parity work when Codex/Fixer must turn one or more large Figma desktop/tablet/mobile frames into a near-pixel-matched website or app. Trigger when the user asks to achieve Figma parity, compare Figma vs current implementation, split a large landing page into sections, create section screenshots, write per-section technical assignments, dispatch Netrunners/agents for visual fixes, export or recover missing assets, and reverify with Playwright/Figma screenshots. Especially useful for large pages where a one-shot implementation exists but needs systematic section-by-section parity.
---

# Figma Frontend Works

## Overview

This skill turns a big Figma mock into a controlled evidence pipeline:

1. split the page into logical sections,
2. capture Figma and current-implementation screenshots per section,
3. write a technical assignment per section,
4. implement each section in a bounded worker pass,
5. regenerate fresh screenshots and review them.

The pattern is intentionally heavier than a normal frontend task. Use it when visual parity matters more than raw speed.

## Core Principle

Use Figma for visual truth and the live/reference product for content truth.

- Figma controls layout, spacing, typography intent, responsive structure, colors, radii, shadows, image placement, and visual hierarchy.
- The current production/reference site controls text, real business content, URLs, counts, active records, and assets when Figma is stale.
- When they disagree, preserve Figma structure but prefer reference-site content unless the Architect explicitly says otherwise.

## Preconditions

Before dispatching implementation workers, establish:

- the target route or screen, such as `/` or a specific app page;
- the local implementation URL, such as `http://localhost:8090/`;
- the reference production URL if content may be stale in Figma;
- selected Figma frames for all relevant breakpoints, usually desktop, tablet, and mobile;
- an artifact root, usually `autonomous_works/`;
- available tools, usually `figma-console-mcp`, `playwright`, and optionally `chrome-devtools`.

If the selected Figma frames are missing or ambiguous, stop and ask the Architect to select them. Do not guess frame identity on a parity pass.

## Phase 1: Section Decomposition

Split the page into stable logical sections. Use human names that map to real page blocks:

```text
autonomous_works/
  header/
  hero/
  services/
  process/
  reviews/
  footer/
```

For each section, plan this structure:

```text
<section>/
  figma/
    desktop.png
    tablet.png
    mobile.png
  website_current/
    desktop.png
    tablet.png
    mobile.png
  ta.txt
  website_after/
    desktop.png
    tablet.png
    mobile.png
```

Run a one-section pilot first. Only scale the workflow after the pilot proves that screenshots, cropping, and review conventions are correct.

## Phase 2: Screenshot Corpus

For each section, capture six baseline screenshots:

- Figma desktop/tablet/mobile into `<section>/figma/`.
- Current implementation desktop/tablet/mobile into `<section>/website_current/`.

Use `figma-console-mcp` for selected frames and nodes when possible. Use `playwright` for current implementation section screenshots. Prefer section-level screenshots, not whole-page screenshots, unless a section is the full viewport/chrome.

Typical viewport widths:

- desktop: viewport around `1240`, content around `1080`;
- tablet: viewport around `848`, content around `768`;
- mobile: viewport around `390`, content around `350`.

Verify every screenshot:

```bash
file autonomous_works/<section>/{figma,website_current}/*.png
sips -g pixelWidth -g pixelHeight autonomous_works/<section>/{figma,website_current}/*.png
```

### Figma Export Fallbacks

Figma export often fails in real runs. Handle it deliberately:

- If direct node export lacks background, crop the section from a full-frame Figma export so the containing background is preserved.
- If `FIGMA_ACCESS_TOKEN` is missing, use existing Figma-derived full-frame screenshots and node bounds.
- If the Desktop Bridge is disconnected, do not hallucinate live data; report it and use accepted local Figma artifacts.
- If transparent/blank backgrounds appear, re-crop from the full frame instead of using isolated node exports.

The best fallback is often: full Figma frame screenshot + metadata/bounds + `magick -crop`.

## Phase 3: Per-Section Technical Assignments

Create one `ta.txt` per section. A good `ta.txt` is implementation-oriented and specific enough for a worker to act without re-litigating the whole page.

Use this structure:

```text
1. Section title
2. Goal
3. Desktop updates
4. Tablet updates
5. Mobile updates
6. Content/data updates
7. Visual/style updates
8. Acceptance criteria
```

The TA worker must compare that section's `figma/*` and `website_current/*` screenshots directly. The Fixer may review only the resulting text for structure, usefulness, scope, and obvious contradictions; it does not need to redo every pixel comparison.

Reject TAs that are:

- based on stale observations no longer visible in screenshots;
- too vague to implement;
- missing breakpoint-specific requirements;
- missing acceptance criteria;
- proposing unrelated redesigns outside the section.

## Phase 4: Implementation Workers

Implement section-by-section. Prefer sequential workers because sections usually share `front-page.php`, helpers, assets, and global CSS. Run parallel workers only when write scopes are truly disjoint.

Each implementation worker receives:

- its section `ta.txt`;
- `figma/{desktop,tablet,mobile}.png`;
- `website_current/{desktop,tablet,mobile}.png`;
- local URL;
- reference URL;
- explicit write scope;
- requirement to generate `website_after` screenshots.

Worker rules:

- Make the smallest defensible fix batch for that section.
- Do not revert other workers' accepted changes.
- Do not modify unrelated sections by drift.
- If assets are missing, try exact source first: reference site, reference API, Figma export, local project assets.
- If exact assets are unavailable, crop from accepted Figma artifacts and document the caveat.
- Prefer real SVG/PNG/photo assets over CSS approximations, but use approximations only when exact assets cannot be recovered.

## Phase 5: Post-Change Verification

Every implementation worker must create:

```text
autonomous_works/<section>/website_after/desktop.png
autonomous_works/<section>/website_after/tablet.png
autonomous_works/<section>/website_after/mobile.png
```

The worker must compare:

- Figma reference vs `website_after`;
- old `website_current` vs `website_after`;
- `ta.txt` acceptance criteria vs final DOM/screenshots.

The worker should iterate before reporting if the first `website_after` pass is materially wrong.

Require at least:

- PHP/JS/CSS syntax checks for touched files;
- Playwright screenshots at desktop/tablet/mobile;
- image validity/dimension checks;
- DOM assertions for content/order/counts when relevant;
- local smoke test if a project-level smoke script exists.

## Fixer Review

The Fixer reviews each worker before accepting:

1. Confirm changed files match the declared scope.
2. Confirm `website_after` has three valid PNGs.
3. Spot-check the screenshots visually.
4. Read residual risks and asset caveats.
5. Run or trust reported syntax checks depending on evidence quality.
6. Accept, repair-fork, or send rework instructions.

At the end, run a coverage check:

```bash
for d in autonomous_works/*; do
  [ -d "$d/website_after" ] && printf '%s ' "$d" && find "$d/website_after" -name '*.png' | wc -l
done
find autonomous_works -path '*/website_after/*.png' -type f | wc -l
```

Then run the project smoke test.

## Recommended Netrunner Prompt Skeleton

Use this shape for section implementation sessions:

```text
Implementation task: update exactly the homepage `<section>` section to satisfy
`autonomous_works/<section>/ta.txt`, then reverify with fresh screenshots.

Rules:
1. Use Figma/screenshots for visual layout/styling/responsive behavior.
2. Use <reference-url> as source of truth for current text/content/assets where Figma is stale.
3. If exact SVG/PNG/photo/icon assets are missing, try to fetch/export exact assets from
   the reference website/API and/or Figma artifacts before approximating.
4. Do not rewrite unrelated sections and do not revert accepted changes.

Verification:
- run syntax/check commands for touched files;
- capture `website_after/{desktop,tablet,mobile}.png`;
- compare against `figma`, `website_current`, and `ta.txt`;
- iterate if materially wrong;
- report changed files, asset sources, checks, screenshot paths/dimensions, residual gaps.
```

## Known Failure Modes And Bypasses

- **Figma bridge disconnected**: use stored Figma artifacts or ask the Architect to reconnect/select frames.
- **Missing `FIGMA_ACCESS_TOKEN`**: crop from accepted Figma screenshots instead of claiming direct export.
- **Node exports miss backgrounds**: crop from the full frame so the parent background is included.
- **Figma content stale**: prefer reference-site content/data while preserving Figma visual structure.
- **Production API exposes fewer records than Figma**: use reference data when content truth matters; use Figma crops only when visual parity requires missing portraits/icons.
- **Shared CSS conflicts**: run section implementation sequentially, not 11 workers at once.
- **Untracked repo or dirty worktree**: avoid destructive git commands; verify through file lists, screenshots, lint, and smoke tests.
- **Exact fonts unavailable**: approximate with existing stack, font weight, letter spacing, transform/scale, and line-height; document the residual gap.
- **Large mobile screenshots differ in height**: accept when the section intentionally shows the full responsive content and no blank tail/overflow remains.
- **Assets only visible inside screenshots**: crop them into local assets, name them clearly, and record source artifact/crop caveat.

## Completion Standard

The pass is complete only when:

- every section has `figma`, `website_current`, `ta.txt`, and `website_after` artifacts;
- every implementation session has been reviewed and accepted;
- all `website_after` screenshots are valid images;
- the local smoke test passes;
- residual caveats are explicit;
- the Architect can manually review the section folders without reconstructing the workflow from chat.
