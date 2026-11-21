# Documentation landing design review

Each round contains complete desktop and mobile designs, the render source, proposal and a SHA-256 inventory. Starting with round 5 the inventory includes four additional frames covering the alternate screenshot details. From round 9, two further frames show the actual-size viewer. The reviewers use the same frozen files. All three reviewers must reach 9/10 in the same round, using the unrounded mean of seven dimensions. The limit is ten rounds. The desktop implementation review and the earlier CLI landing review are separate records.

[Raw ballots and task references](scores.json) use the rubric order listed in the file. The T3 task references have the common prefix `node:delegated-task:command%3Amcp%3Aebe8a248-d468-418b-a5c0-bef10584aeef%3Adelegate-task%3A` followed by the recorded seat-specific ID.

## Council decision

The review finished after ten rounds. No round reached 9 from all three reviewers. Round 6 ranks highest by its lowest reviewer score. The existing landing remains in use.

Highest-ranked candidate · [desktop](round-06/desktop.png) · [mobile](round-06/mobile.png) · [proposal and interaction details](round-06/proposal.md).

| Round | Astra | Gemini | Kimi |
| --- | ---: | ---: | ---: |
| 1 | 9.0857 | 8.5429 | 8.6143 |
| 2 | 9.1000 | 8.4429 | 8.8000 |
| 3 | 9.1000 | 8.8000 | 9.1000 |
| 4 | 9.4143 | 8.7857 | 9.1000 |
| 5 | 9.1429 | 8.6714 | 8.8286 |
| 6 | 9.1286 | 8.9714 | 9.1571 |
| 7 | 9.3286 | 8.9429 | 9.0571 |
| 8 | 9.2429 | 8.8143 | 8.8143 |
| 9 | 9.3000 | 8.8143 | 9.2143 |
| 10 | 9.2571 | 8.5714 | 9.1714 |

Means are displayed to four decimal places. The threshold was applied before rounding. The [ballots](scores.json) include every dimension, reviewer task and round finding.

<details>
<summary>Scores by dimension</summary>

| Round | Reviewer | Typography | Hierarchy | Spacing & density | Color & contrast | Iconography | Consistency & polish | Clarity | Overall |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | Astra | 9.2 | 9.3 | 9.0 | 9.4 | 9.1 | 8.9 | 8.7 | 9.0857 |
| 1 | Gemini | 8.7 | 8.8 | 8.2 | 9.2 | 8.7 | 8.1 | 8.1 | 8.5429 |
| 1 | Kimi | 8.9 | 8.9 | 8.4 | 8.6 | 8.7 | 8.2 | 8.6 | 8.6143 |
| 2 | Astra | 9.3 | 9.1 | 8.9 | 9.4 | 9.1 | 9.1 | 8.8 | 9.1000 |
| 2 | Gemini | 8.6 | 8.5 | 8.1 | 8.8 | 8.9 | 8.2 | 8.0 | 8.4429 |
| 2 | Kimi | 9.0 | 9.1 | 8.9 | 8.8 | 8.5 | 9.0 | 8.3 | 8.8000 |
| 3 | Astra | 9.1 | 9.2 | 9.0 | 9.4 | 9.0 | 9.2 | 8.8 | 9.1000 |
| 3 | Gemini | 8.8 | 8.9 | 8.6 | 9.2 | 9.0 | 8.5 | 8.6 | 8.8000 |
| 3 | Kimi | 9.1 | 9.3 | 8.9 | 9.2 | 9.0 | 9.2 | 9.0 | 9.1000 |
| 4 | Astra | 9.5 | 9.5 | 9.3 | 9.6 | 9.4 | 9.4 | 9.2 | 9.4143 |
| 4 | Gemini | 8.6 | 8.7 | 8.8 | 9.4 | 9.0 | 8.4 | 8.6 | 8.7857 |
| 4 | Kimi | 9.2 | 9.3 | 9.0 | 9.1 | 8.9 | 9.2 | 9.0 | 9.1000 |
| 5 | Astra | 9.3 | 9.4 | 9.0 | 9.4 | 9.0 | 9.2 | 8.7 | 9.1429 |
| 5 | Gemini | 9.0 | 8.8 | 8.2 | 9.3 | 8.7 | 7.9 | 8.8 | 8.6714 |
| 5 | Kimi | 9.0 | 9.1 | 8.8 | 8.9 | 8.8 | 9.0 | 8.2 | 8.8286 |
| 6 | Astra | 9.1 | 9.3 | 8.8 | 9.4 | 9.1 | 9.0 | 9.2 | 9.1286 |
| 6 | Gemini | 9.1 | 9.0 | 8.3 | 9.5 | 9.2 | 8.6 | 9.1 | 8.9714 |
| 6 | Kimi | 9.2 | 9.4 | 8.9 | 9.4 | 9.1 | 9.3 | 8.8 | 9.1571 |
| 7 | Astra | 9.4 | 9.3 | 9.2 | 9.6 | 9.3 | 9.4 | 9.1 | 9.3286 |
| 7 | Gemini | 9.3 | 9.2 | 8.6 | 9.4 | 8.8 | 8.5 | 8.8 | 8.9429 |
| 7 | Kimi | 9.1 | 9.3 | 9.0 | 9.1 | 9.0 | 9.2 | 8.7 | 9.0571 |
| 8 | Astra | 9.2 | 9.4 | 8.9 | 9.5 | 9.3 | 9.3 | 9.1 | 9.2429 |
| 8 | Gemini | 9.1 | 8.8 | 8.3 | 9.4 | 9.0 | 8.4 | 8.7 | 8.8143 |
| 8 | Kimi | 8.8 | 9.1 | 8.5 | 8.9 | 8.6 | 9.1 | 8.7 | 8.8143 |
| 9 | Astra | 9.4 | 9.3 | 9.0 | 9.6 | 9.2 | 9.4 | 9.2 | 9.3000 |
| 9 | Gemini | 9.1 | 8.9 | 8.8 | 9.4 | 8.5 | 8.3 | 8.7 | 8.8143 |
| 9 | Kimi | 9.3 | 9.4 | 9.0 | 9.3 | 9.1 | 9.4 | 9.0 | 9.2143 |
| 10 | Astra | 9.3 | 9.5 | 9.0 | 9.5 | 9.2 | 9.2 | 9.1 | 9.2571 |
| 10 | Gemini | 8.8 | 8.6 | 8.2 | 9.1 | 8.5 | 8.1 | 8.7 | 8.5714 |
| 10 | Kimi | 9.3 | 9.5 | 9.1 | 9.0 | 9.2 | 9.2 | 8.9 | 9.1714 |

</details>

## Documentation validation

The rebuilt desktop guide loaded all four native captures in a browser at 1440 × 900 and 390 × 844. Keyboard navigation and the mobile guide menu reached the selected guide. A follow-up mobile check on Diagnostics confirmed that long identifiers stay intact and wide tables scroll within the article.

The static build produced eleven HTML pages. All 299 local link, image and fragment references resolved. The desktop Markdown, MDX and full-text exports each resolve all four embedded native screenshots. TypeScript checks passed.

## Reproduce a design

Run the renderer from the repository root with Python, Playwright 1.59.0 and Chromium headless shell 156.0.8078.4 (macOS arm64 for these captures). The page loads the repository's DM Sans font and the original native Home screenshot. Rounds 1 and 2 also use a 322 × 155 crop beginning at (598, 147). From round 7, the Project view uses a second native capture showing the bound checkout and saved Profile. Round 10 adds the native Sync queue capture to show the selected conflicted record. The shared inputs and renderer are recorded in [render-inputs.sha256](render-inputs.sha256).

```sh
python plans/documentation-landing/render.py plans/documentation-landing/round-01 --chromium /path/to/chrome-headless-shell
(cd plans/documentation-landing/round-01 && shasum -c SHA256SUMS)
python3 .agents/skills/council/scripts/score.py plans/documentation-landing/scores.json 9
```

The native window was captured at 1280 × 800 from revision `93bc2d0`. Its example workspace, device, and conflict are recorded in the [native capture manifest](../../docs/development/desktop/screenshots/native-redesign-93bc2d0/manifest.json).
