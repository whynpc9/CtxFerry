# Documentation

[User guides](content/index.md) cover CLI workflows available in the current source tree. Use `docs/content/` as the content source for the Nimbus documentation site. Add reader-facing guides and their assets there.

## Local development

- [User documentation plan](development/documentation-plan.md) maps each public page to a reader task and records the desktop revision covered.
- [Context storage and query contracts](development/context-storage.md) describes schema migrations, indexing and response limits.
- [Sync protocol and test transport](development/sync-protocol.md) describes the Git format, reconciliation rules and fault-injection scenarios.
- [Desktop acceptance](development/desktop/acceptance-28.md) covers the Overview, sync queue, remote checks and confirmed backups.
- [Initial desktop implementation review](development/desktop/review-28.md) records the tested revision, screenshots and findings.
- [Issue tracking](agents/issue-tracker.md), [triage labels](agents/triage-labels.md) and [domain documentation](agents/domain.md) describe contributor workflows.

Keep implementation notes and validation evidence in `docs/development/`, agent instructions in `docs/agents/`, and architecture decisions in `docs/adr/` as they are adopted. These directories sit outside the site's content source.

## Preview the site locally

Use Node.js 24. From the repository root, build and serve the static site:

```sh
cd docs/site
npm ci
npm run build
npm run preview
```

Open `/CtxFerry/` on the preview server for the landing page, or `/CtxFerry/docs/`
for the user guide. Edit guide pages in `docs/content/`; the site reads them
directly and resolves their Markdown links at build time.

The landing page uses round 7 of the design council's frozen candidates
(archive SHA-256 `38cc3497f59d66d6dcca246e63caccd7ccd1ec7f55ee976f07cc575d0f6d9b2b`).
It had the highest minimum score among rounds with all three ballots. The
review ended in round 8 without a three-seat pass at 9.5.
