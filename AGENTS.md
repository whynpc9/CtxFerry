# AGENTS.md

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `whynpc9/CtxFerry`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage labels are used unchanged: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root, created lazily. See `docs/agents/domain.md`.

### Desktop UI council

For independent, scored reviews of the MyGo Native UI design, use the project [council skill](.agents/skills/council/SKILL.md).

After completing a desktop UI task, capture the implemented screens from the running application using an isolated workspace. Give the screenshots, their data state, and the matching design frames to a Kimi K3.1 Pro subagent for independent review. Record the findings against the implementation revision that produced the screenshots.
