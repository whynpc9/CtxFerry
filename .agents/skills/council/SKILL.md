---
name: council
description: Coordinate independent, scored reviews of CtxFerry MyGo Native UI designs. Freeze each round, reconcile feedback and select the final design under the user's scoring and round limits.
---

# CtxFerry UI Council

Read the [desktop functional specification](../../../plans/desktop-information-architecture.md) for redesign scope and flows. Use the user's designer, reviewer models, thresholds, round limits and commit scope.

## Freeze each round

1. Number the round and render its principal destinations and key states with the current design's native MyGo fixture. Save the design proposal, original PNGs, fixture source and pinned dependencies together. List each file and its SHA-256 in the round's manifest.
2. Inspect the frames, text and wrapping. Trace the same Project, device, revision, scope and receipt across the corresponding flows. Archive the snapshot and verify that it matches what reviewers will see. Keep snapshots recoverable until selection.
3. If a reviewer has image limits, provide contact sheets in reading order with a manifest and the full-resolution frames for detail. Recover any unreadable frames before counting the ballot.

## Run independent reviews

Check the available reviewer model IDs and settings before delegation. The usual seats are GPT-6.2 Astra with high reasoning, Gemini 4 Neon with high reasoning and K3.1 Pro with max thinking.

Give each seat a fresh read-only task for the same frozen snapshot. Include the proposal, functional spec, frame manifest and rubric. Keep thresholds, earlier scores and other reviewers' opinions outside the brief. Record each task ID and requested model settings. Follow delegation completion notifications. For a recoverable failure, retry the same seat on the same snapshot.

Ask each reviewer to inspect every listed frame and score Typography, Hierarchy, Spacing & density, Color & contrast, Iconography, Consistency & polish and Clarity from 0 to 10 in tenths. Request up to three material findings with locations and user impact. Ask the reviewer to name any frame they could not inspect.

## Score and select

- Compare the equal-weight arithmetic mean with the user's threshold before rounding. Use [score.py](scripts/score.py) to calculate means from a JSON object with a `rounds` list. Each round maps `Astra`, `Gemini` and `Kimi` to seven scores in rubric order, a matrix of seven scores per frame, or `null` while pending. Pass the applicable threshold as the second argument. Display seven dimensions and the Overall when the round is complete.
- Send the designer actionable findings with frame locations, impact and relevant functional requirements. Freeze the revision before starting the next independent review.
- Apply the requested stopping rules. If the round limit or a provider blocker ends the run before a pass, report the reason and the best fully scored round. Rank those rounds by highest minimum reviewer mean, then highest average mean, then latest round. Retain the selected proposal, PNGs and reproducible fixture, and record outstanding findings.
