# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`GLOSSARY.md`** at the repo root, or
- **`GLOSSARY-MAP.md`** at the repo root if it exists: it points at one `GLOSSARY.md` per context. Read each one relevant to the topic.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

This repository uses a **single-context** domain-docs layout. The planned application/package monorepo shares one domain vocabulary; application and package boundaries do not automatically define separate domain contexts.

```text
GLOSSARY.md
docs/
    adr/
        0001-<decision>.md
        0002-<decision>.md
apps/
packages/
```

The application/package directories illustrate the planned structure, not directories created by this setup.

The glossary and ADRs are created lazily, not as empty setup placeholders. A future multi-context layout uses a root `GLOSSARY-MAP.md` pointing to one `GLOSSARY.md` per context, with system-wide ADRs in `docs/adr/` and context-scoped ADRs in `src/<context>/docs/adr/`.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `GLOSSARY.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_
