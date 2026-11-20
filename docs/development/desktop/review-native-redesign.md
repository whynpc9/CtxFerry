# Native desktop redesign implementation review

## Tested revisions

- Initial capture and independent review: `f0135021a3f713d9d848a06aeb754127cc4b6ef9`
- Findings fixed and independently rechecked: `93bc2d0f47f00e589c17000a6a501618510df5a3`

The final capture is in [screenshots/native-redesign-93bc2d0](screenshots/native-redesign-93bc2d0), with the full data state and per-image SHA-256 values in its [manifest](screenshots/native-redesign-93bc2d0/manifest.json). Contact sheets 1–4 cover all 22 final frames.

The captures came from running MyGo native windows through `desktop_smoke` automation, not from the design fixture. `capture_redesign.py` built the CLI and desktop, created an isolated synthetic Workspace through real operations, and exercised the production views. The fixture included one real Context conflict, a confirmed private Git history, a Device Configuration override, a mapped database and credential reference, an actual TCP probe, two local artifacts, and an installed Skill with both a newer approved pin and local file drift. Provider values were synthetic and no real provider or user Workspace was accessed.

## Independent review

The independent K3.1 Pro reviewer (`agent-0`) inspected all 22 initial frames at native resolution and the matching round 10 design frames. It found no blockers and resolved every outstanding design-council content finding.

| Dimension | Score |
| --- | ---: |
| Typography | 8.9 |
| Hierarchy | 8.7 |
| Spacing and density | 8.3 |
| Color and contrast | 9.2 |
| Iconography | 9.1 |
| Consistency and polish | 8.2 |
| Clarity | 8.7 |

Unrounded mean: `61.1 / 7 = 8.728571428571…`.

The reviewer then inspected all seven affected final frames at native resolution. It marked every implementation finding resolved and found no new material regression. Scores were not changed.

### Implementation findings and fixes

- Scroll-edge slicing in wide Backup and compact Configuration/Conflict: the Configuration preview now opens beside the field list on wide windows and as the primary compact panel. Final decision pages were captured at their actual bottom, so the complete preview, acknowledgement and controls are visible. Final `05`, `16`, `19`, `20` and `21` passed independent recheck.
- `Adopt configured session` was truncated in Credentials: provider actions now use full-width rows. Final `08` shows the complete action.
- `Unknown environment` was clipped in Resources: an unknown system now reads `Unknown` without ellipsis. Final `07` passed recheck.

### Round 10 council findings

- Provider-held credentials and Git history are explicitly named in Backup exclusions and plaintext exposure. The receipt repeats the exclusions (`16`, `17`, `21`).
- Skill installation receipts and installed files are included in both the reviewed and recorded exclusions (`16`, `17`, `21`).
- Local-only exposure is one clearly labelled optional group (`16`, `21`).
- The Task identifies the exact TCP target and observation timestamp (`06`), repeated by the Resource view (`07`).
- Skills expose both installed-to-local drift and installed-to-candidate diffs (`10`, `11`).
- Queue status and object icons are separate. The supplied real state had no blocked Task, so the separate blocked-Task row was not visually exercised; the implementation renders blocked state as its own pill when present.

## Verification

The following passed on the implementation:

- `go test ./...` from the repository root
- `go test ./...` and `go test -tags ctxferry_test ./...` from `cmd/ctxferry-desktop`
- Native Configuration preview/cancel/apply and stale-token tests
- Real two-device Conflict resolution, reread, peer publication and receipt tests
- Compact Backup review and actual manifest write tests
- Real mapped-Resource TCP target and timestamp tests
- Native capture at 1280×800 and 1024×680, including a real Backup receipt

Reproduce the capture into a new `/tmp` directory:

```sh
cd cmd/ctxferry-desktop
python3 capture_redesign.py /tmp/ctxferry-redesign-<revision> --revision <full-sha>
```
