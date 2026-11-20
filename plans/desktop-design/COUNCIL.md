# Desktop redesign council

Round 10 is the archived design candidate. The 10-round cap was reached. The lowest of its two reviewer scores was the highest across the run. The 9.5 approval threshold was not met.

Gemini returned no ballot in rounds 1 and 2 and was removed at the user’s request. Both continuing reviewers cleared 9.0 in round 2, raising the two-seat threshold to 9.5 from round 3.

| Round | GPT-6.2 Astra · High | K3.1 Pro · Thinking Max |
| ---: | ---: | ---: |
| 1 | 8.6714 | 8.4571 |
| 2 | 9.1000 | 9.1286 |
| 3 | 9.1714 | 9.1714 |
| 4 | 9.3000 | 8.9143 |
| 5 | 9.2571 | 8.9143 |
| 6 | 9.1429 | 9.0000 |
| 7 | 9.2571 | 8.7571 |
| 8 | 9.1286 | 8.7286 |
| 9 | 9.3000 | 9.0714 |
| 10 | 9.3429 | 9.2571 |

## Round 10 findings

### Astra

- Frames 29/30/34: name provider-held credentials as the exclusion, and list Git history among plaintext contents. Plaintext can contain embedded secrets.
- Frame 11: identify the database as the specific probe target and retain its timestamp.
- Frame 16: show installed-to-candidate diff as well as installed-to-local drift.

### Kimi

- Frames 29/34: separate blocked status from the included Task icon.
- Frame 30: include Skill receipts in the reviewed and recorded exclusions.
- Frame 29: keep optional exposure in one clearly labelled group.

Both reviewers inspected all 36 native design frames. The fixture passed overflow, ellipsis and pinned-bar checks in both rendered sizes and 45 contrast-pair checks. The backup variant without local-only data is documented in the proposal.

[Design proposal](README.md) · [Frozen manifest](manifest.json) · [Run record](council-run.json) · [Ballots](council-scores.json)
