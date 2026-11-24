# Glossary

Domain terms for CtxFerry's internals. Product terms (Workspace, Project, Checkout, Profile, Task, Resource, SecretBinding, Skill, Artifact) are defined in `PRD.md` and `docs/content/`.

**Record kind**
A type of published record (`project`, `repository`, `membership`, `context`, `configuration`, `resource`, `task`, `secret-binding`, `artifact`, `skill-pin`, `skill-group`). Each kind is one `recordKind` entry in `internal/app/record_kind.go`. The entry covers how the kind is decoded, validated, installed, exported and planned. Sync code iterates the registry and never switches on kind names.

**Sync backend**
The seam between the sync protocol and the private remote (`syncBackend` in `internal/app/sync_backend.go`). It owns every Git and GitHub operation in a sync round. Production uses managed Git; package tests use an in-memory remote.
_Avoid_: transport (the backend also stages candidates and measures history).

**Catalog mutation**
A local write that may change shared records. It goes through `mutateCatalog`, which queues the change for sync in the same transaction.

**Live record**
A catalog row with no sync tombstone for its own ID or the IDs it depends on. `live()` is the one SQL form of this rule.

**Context show budget**
The 8192-byte limit on one agent-facing `context show` page. `FitContextPage` enforces it. The caller measures its own rendering of the page.

**Skill install**
One Skill target's lifecycle: plan, confirm, stage, verify, publish, finalize, and recovery of an interrupted replacement (`internal/app/skill_install.go`).

**Standalone credential**
A credential requirement with an empty `resourceSlot`, served only by a SecretBinding with an empty `resourceId`. No Resource backs it, so it has no slot mapping or reachability. See `docs/adr/0001-standalone-credentials.md`.
_Avoid_: synthetic Resource.

**Default Profile**
The Project's Profile named `default`. Credential and Resource resolution falls back to it when no Profile is given or selected; `secret bind` and `onboard import-env` create it when the Project has no Profiles.
