# AGENTS.md instructions

Act like a senior engineer doing a pragmatic patch, not a framework designer.

Requirements:
- Solve the requested problem with the minimum necessary complexity.
- No speculative architecture.
- No future-proofing unless explicitly requested.
- No new abstraction for one call site.
- No helper extraction unless it improves clarity immediately.
- No broad try/catch, silent defaults, or compatibility shims unless required.
- Preserve external contracts.
- Prefer deleting complexity over adding it.
- If a larger refactor seems beneficial, mention it separately but do not do it unless asked.
- Before committing or pushing, run the relevant verification loop for every touched area. Prefer the same command and pinned tool version CI uses; if a local tool is missing, use the repo's Docker/devcontainer path or state clearly that verification could not be run.
