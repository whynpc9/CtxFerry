---
name: ctxferry
description: Discover the current Project's context, resources, and registered Tasks with the CtxFerry CLI.
---

# CtxFerry

Start in the current checkout with `ctxferry context show --format json`. Use the index to retrieve only the context needed for the work. For example:

- `ctxferry context get <document-uuid> --format json` retrieves a document subject to its disclosure policy.
- `ctxferry task inspect <name-or-uuid> --format json` shows a registered Task and its requirements.
- `ctxferry doctor --format json` checks this device's setup when context or execution is unavailable.

Machine-readable queries return one JSON object. When asked to execute a registered Task, use `ctxferry run <task> --format jsonl` for structured execution events. Use only the configured credential bindings and device-local trust decisions. Never pass credential values as arguments or copy them into agent context.
