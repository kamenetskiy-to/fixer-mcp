---
name: design-system-works
description: "Run the greenfield design-system-first pipeline for Flutter MVP UI work with shadcn_ui: create design-system and screen specs first, then generate schematic review screenshots per screen. Use before implementation when the user has a product brief and wants mobile, desktop, or universal responsive app UI direction."
---

# Design System Works

Use this skill for greenfield MVP UI planning where the desired output is a reusable design system, screen specs, and schematic screenshots for human review before any real Flutter screens are implemented.

This flow is design-system-first. It is related to `figma-frontend-works`, but it is not a Figma parity pipeline:

- `figma-frontend-works` starts from existing Figma frames and compares implementation screenshots against them.
- `design-system-works` starts from a product brief, builds a source-of-truth Markdown design system, decomposes screens, then creates schematic screenshots as review references.

## Inputs

Collect or confirm:

- product description;
- target users and core jobs;
- functional screens and features;
- target form factor: `mobile`, `desktop`, or `responsive` universal;
- required MVP states: default, loading, empty, error, success, expanded, permission-denied, offline, or other domain states;
- brand constraints, if any;
- output project slug for `design_works/<project>/`.

If the form factor is omitted, default to `responsive` universal and include both mobile and desktop notes.

## Reference Stack

Use the local `shadcn-ui-flutter` skill as the component vocabulary. It already contains theming, typography, responsive guidance, and per-component docs.

Public visual references are enough for component appearance:

- https://pub.dev/packages/shadcn_ui
- https://mariuti.com/flutter-shadcn-ui/

Do not build a component screenshot harness for this MVP pipeline. Screenshot workers should cite the exact local skill docs and public component pages they used.

## Taste Rule

The Markdown spec is the source of truth.

Screenshots are human-review references, not implementation drivers. If a screenshot conflicts with `design-system.md` or a `screens/<screen>.md` spec, the Markdown wins and the screenshot must be regenerated or annotated as stale.

## Step 1: Design-System Netrunner

Dispatch one Netrunner to create the design system and decompose screens.

Recommended model: `codex` + `gpt-5.6-luna`.

Owned files:

```text
design_works/<project>/
```

Required outputs:

```text
design_works/<project>/
  design-system.md
  screens/
    <screen>.md
```

The Netrunner must use the design-system prompt pattern:

```text
You are designing a greenfield MVP design system for a Flutter app built with shadcn_ui.

Input: product description, target users, functional screens/features, target form factor, and brand constraints.

Build design-system.md first. Do not design screenshots or implementation code yet.

Use the local shadcn-ui-flutter skill as the component vocabulary and public shadcn_ui docs only as visual references.

Output product UX posture, screen inventory, design tokens, component inventory, composition rules, and one screens/<screen>.md spec per screen.

Hard rule: Markdown design-system and screen specs are the source of truth. Future screenshots are review references only.
```

`design-system.md` must include:

- product UX posture: audience, use context, density, tone, accessibility posture;
- screen inventory and navigation model;
- color roles mapped to shadcn_ui theme concepts;
- type scale and semantic text roles;
- spacing scale;
- radius, border, surface, and elevation strategy;
- motion and feedback rules;
- component inventory using real shadcn_ui components only;
- responsive rules for mobile, desktop, or universal layout;
- cross-screen state rules.

Each `screens/<screen>.md` must include:

- user goal;
- route/screen role;
- layout structure for mobile and/or desktop;
- shadcn_ui components by name;
- content/data placeholders;
- default, loading, empty, error, and success states where relevant;
- validation and interaction behavior;
- accessibility notes;
- screenshot generation notes.

## Step 2: Parallel Screenshot Wave

After Step 1 is complete and reviewed for obvious gaps, dispatch a parallel wave of N Netrunners where N equals the number of screen specs.

Recommended model: `codex` + `gpt-5.6-luna` with native image generation and image viewing.

Each screenshot Netrunner receives:

- `design_works/<project>/design-system.md`;
- exactly one `design_works/<project>/screens/<screen>.md`;
- local `shadcn-ui-flutter` reference path;
- public shadcn_ui visual references;
- target form factor and required states.

Files owned by each worker:

```text
design_works/<project>/screens/<screen>/
```

Required outputs:

```text
design_works/<project>/screens/<screen>/
  default.png
  <state>.png
  states.md
```

For mobile scroll screens, create tall screenshots that show the full scrollable composition. For desktop screens, create a full app-surface screenshot with the navigation and content density specified in Markdown.

Workers must self-iterate:

1. generate screenshot from the screen spec;
2. view the image;
3. compare it against `design-system.md` and `screens/<screen>.md`;
4. adjust or regenerate until it is adequate for human review;
5. document remaining caveats in `states.md`.

Use this screenshot prompt pattern:

```text
Generate one clean schematic screenshot for human review of a Flutter shadcn_ui screen.

Inputs: design-system.md, one screens/<screen>.md source-of-truth spec, target form factor, required state, and shadcn_ui visual references.

Make a Figma-like low-to-mid fidelity screen mockup. Use the real screen structure and shadcn_ui component names from the spec.

Keep it schematic: grayscale or very muted token hints only; no decorative illustration; no final marketing polish.

Use labeled UI regions for boxes, table rows, tabs, forms, cards, sheets, dialogs, menus, badges, and other shadcn_ui-like primitives.

Do not invent components not present in the Markdown spec. Do not treat the screenshot as implementation truth.

After generation, view the image and iterate until it is adequate for human review.
```

`states.md` must include:

- generated screenshots and their intended states;
- component references used;
- whether mobile output is a tall scroll screenshot;
- known mismatches or ambiguity;
- confirmation that Markdown remains source of truth.

## Review Checklist

Before handing off for actual Flutter implementation, confirm:

- `design-system.md` exists and covers tokens, components, layout, responsive rules, states, and accessibility;
- every listed screen has a `screens/<screen>.md` spec;
- every screen spec uses real shadcn_ui components or explicitly marks a necessary custom component;
- screenshot workers generated PNGs and viewed them;
- `states.md` exists for every screenshot folder;
- screenshots do not introduce components, states, or layout hierarchy that contradict Markdown;
- public gallery and local skill references are recorded;
- implementation has not started in this pipeline run.

## Output Directory Contract

Use this shape:

```text
design_works/<project>/
  design-system.md
  references/
    README.md
  screens/
    <screen>.md
    <screen>/
      default.png
      loading.png
      empty.png
      error.png
      states.md
```

The `references/` folder is optional. Use it only for brief notes, linked public references, or small project-specific assets. Do not copy large external docs or build generated component catalogs there.

## Non-Goals

- Do not generate production Flutter UI code.
- Do not create real project screens.
- Do not merge with the Figma parity workflow.
- Do not build or maintain a component screenshot harness.
- Do not let screenshots override Markdown specs.
