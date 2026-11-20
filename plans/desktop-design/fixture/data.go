package main

import (
	"fmt"
	"strings"
)

// Synthetic, internally consistent Workspace used by every frame.
// Nothing here is read from a real data root.

const (
	wsName   = "Northwind Labs"
	wsID     = "5b0e2c91-7d43-4f6a-9a1e-3c8d27f04b6e"
	dataRoot = "/home/dev/.local/share/ctxferry"

	deviceName = "studio-linux"
	deviceID   = "c4d7a209-1e58-4b3c-8f02-9a6e5d1b7c34"
	deviceOS   = "Ubuntu 26.04 · x86_64"
	otherDev   = "travel-mac"
	otherDevID = "8e2f6b13-4c90-47d5-b1a8-02f7c3e95d61"
	ciDev      = "ci-runner-eu"

	catalogSchema = "Catalog schema 14"
	readAt        = "10:42:08"

	remoteURL     = "git@github.com:northwind/ctxferry-workspace.git"
	branch        = "ctxferry-v1"
	confirmedSHA  = "8c41d2e"
	confirmedAt   = "Today 10:31"
	historyCommit = "412 commits · 18.4 MB"

	previewID    = "7c1e93a4-58d2-4b0f-a6e3-91d40c27f85b"
	journalID    = "e93b0c57-2a1f-4d68-8c3e-6f05a9b2d714"
	conflictTok  = "a51f7e08-3c92-4b6d-9e14-d82c60f3b759"
	pendingRecID = "2f9a6c14-b07e-4d35-a8c1-5e3d90b7f26a"
)

type project struct {
	name, id, sub string
	subscribed    bool
}

var (
	prjAtlas   = project{"atlas-api", "9f3a7c21-6b48-4e0d-b5a2-1c7e84d39f60", "Service API and its runbooks", true}
	prjBilling = project{"billing-worker", "3b8e5d02-9c71-4a46-8f3b-d24e1a60c7f9", "Invoice queue worker", true}
	prjDocs    = project{"docs-site", "d51c0a8e-2f37-4b92-a6d4-7e18c95b3a02", "Public documentation", true}
	prjML      = project{"ml-notebooks", "61e4f9b7-0a2d-4c85-93e1-b8f6027d4a5c", "Research notebooks", false}
)

type checkout struct {
	path, id, profile string
	available         bool
}

var (
	chkAtlas   = checkout{"~/code/atlas-api", "0e7b4d92-5a13-4c68-b9f1-3d82a6c05e47", "staging", true}
	chkReview  = checkout{"~/code/atlas-api-review", "6a2c8f15-d94e-4b07-8e36-1f5b0c9a7d28", "prod-readonly", true}
	chkBilling = checkout{"~/code/billing-worker", "b3f61e8a-07c4-4d25-9a9b-6e2d1c84f053", "dev", true}
	chkDocs    = checkout{"/mnt/ext/docs-site", "47d0a3c6-e815-4f92-b27a-c9e60f1d8b35", "default", false}
)

const (
	resPG     = "postgres-primary"
	resPGID   = "a07d3e5b-6f12-4c89-9b40-e25c18f7d6a3"
	resCache  = "redis-cache"
	taskName  = "deploy-preview"
	taskID    = "f28c6b03-9d4a-4e71-a5c2-087e3b1fd964"
	reqDBPass = "db-password"
	skillName = "release-notes"
	runID     = "run 7781"
)

func short(id string) string { return id[:8] }

const (
	resCacheID = "3e1f8b62-d4a7-4c09-b5e3-7a20c916f84d"
	newSHA     = "9a2e7f1"
)

// snap is the evidence one frame was "read" with. The sidebar footer, the
// top bar and the page all derive their service state, confirmed commit,
// read time and queue counts from the same snap, so they cannot disagree.
// joinedAt is when join wrote the catalog on this device (frame 02b).
const joinedAt = "10:43:20"

type snap struct {
	read        string // local read time shown in the top bar
	service     string // "running", "stopped", "absent"
	confirmed   string // confirmed commit, "" before the first publication
	confirmedAt string
	waiting     int
	conflicts   int
	blocked     int
	offline     bool
	unknown     bool // a publication journal has an unknown outcome
	stale       bool // last refresh failed; snapshot kept
	gate        bool // no Workspace opened yet
}

func (s snap) total() int     { return s.waiting + s.conflicts + s.blocked }
func (s snap) attention() int { return s.conflicts + s.blocked }

// attentionText is the one wording of the Sync badge everywhere: conflicts
// plus blocked records, which need a person. Waiting records are not
// counted; waitingClause says what, if anything, holds them.
func (s snap) attentionText() string {
	if s.attention() == 1 {
		return "1 needs attention"
	}
	return fmt.Sprintf("%d need attention", s.attention())
}

func (s snap) attentionDetail() string {
	var parts []string
	switch {
	case s.conflicts == 1:
		parts = append(parts, "1 conflict")
	case s.conflicts > 1:
		parts = append(parts, fmt.Sprintf("%d conflicts", s.conflicts))
	}
	if s.blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", s.blocked))
	}
	if w := s.waitingClause(); w != "" {
		parts = append(parts, w)
	}
	return strings.Join(parts, " · ")
}

// waitingHold says what holds waiting records back, from the same state that
// gates the publication actions. "" means the running service sends them.
func (s snap) waitingHold() string {
	switch {
	case s.unknown:
		return "held until the uncertain journal is reconciled"
	case s.offline:
		return "held while the remote is unreachable"
	case s.confirmed == "":
		return "held until the first publication"
	case s.service != "running":
		return "held until Sync runs"
	}
	return ""
}

// waitingClause is the one description of waiting records. It is empty
// when nothing waits, and it never promises automatic publication when the
// service is stopped, the remote is offline or a journal is uncertain.
func (s snap) waitingClause() string {
	if s.waiting == 0 {
		return ""
	}
	if h := s.waitingHold(); h != "" {
		return fmt.Sprintf("%d waiting, %s", s.waiting, h)
	}
	return fmt.Sprintf("%d waiting, queued for the running service", s.waiting)
}

// serviceLabel is the short, capitalized form used in pills.
func (s snap) serviceLabel() string {
	_, t := s.serviceText()
	t = t[len("Sync service "):]
	return strings.ToUpper(t[:1]) + t[1:]
}

func (s snap) serviceText() (tone, string) {
	switch s.service {
	case "running":
		return toneOk, "Sync service running"
	case "stopped":
		return toneUnknown, "Sync service stopped"
	}
	return toneUnknown, "Sync service not installed"
}

// Timeline of the synthetic device. Frames name the snapshot they show.
// The inventory is ordered by destination; this list is ordered by time.
var (
	// Setup day, 3 Nov: first publication on this device. No service yet.
	snapSetup     = snap{read: "3 Nov 10:11:02", service: "absent"}
	snapSetupLost = snap{read: "3 Nov 10:13:40", service: "absent", unknown: true}
	snapJoined    = snap{read: "10:43:31", service: "absent", confirmed: "8c41d2e", confirmedAt: "10:31"}
	// Today 09:02: the service was stopped overnight; three ordinary edits wait.
	snapMorning = snap{read: "09:02:15", service: "stopped", confirmed: "5d07b3a", confirmedAt: "yesterday 18:12", waiting: 3}
	// 10:42: before the Configuration edit.
	snapEdit = snap{read: "10:42:08", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 5, conflicts: 1, blocked: 2}
	// 10:44:52 the edit is saved; 10:44:53 the reread shows record 2f9a6c14 waiting.
	snapSaved = snap{read: "10:44:53", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 6, conflicts: 1, blocked: 2}
	// 10:46: the main reading state of most frames.
	snapNow = snap{read: "10:46:20", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 6, conflicts: 1, blocked: 2}
	// 10:49:12 a refresh failed; the 10:46:20 snapshot is kept.
	snapStale = snap{read: "10:46:20", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 6, conflicts: 1, blocked: 2, stale: true}
	// 10:51:07 after keeping the local side: the conflict and its dependent
	// index both become Waiting.
	snapResolved = snap{read: "10:51:07", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 8, conflicts: 0, blocked: 1}
	// 11:04:12 the service pushed 8 records and lost the acknowledgement offline.
	snapOffline = snap{read: "11:06:02", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 8, blocked: 1, offline: true, unknown: true}
	snapOnline  = snap{read: "11:12:05", service: "running", confirmed: confirmedSHA, confirmedAt: "10:31", waiting: 8, blocked: 1, unknown: true}
	// 11:12:40 reconciled: journal cleared, new commit reread at 11:12:41.
	snapCleared = snap{read: "11:12:41", service: "running", confirmed: newSHA, confirmedAt: "11:12:40", waiting: 0, blocked: 1}
)

const (
	staleFailedAt = "10:49:12"
	routineJrnlID = "3d8f1a62-b4c7-4e09-a5d2-8f6c03e1b7a9"
	redisBindID   = "6c2d9e41-0b7a-4f53-8e18-d4a95c3b02f7"
	backupDir     = "/home/dev/backups/ctxferry-2026-11-20"
)

// Execution trust of atlas-api's Tasks for each Checkout. Project overview
// counts and the Tasks list both read this table.
type trustState int

const (
	trustNone    trustState = iota // never trusted on this device
	trustOK                        // trusted definition unchanged
	trustChanged                   // definition changed since trusted: recheck
)

type taskTrust struct {
	name, purpose string
	state         trustState
}

var trustByCheckout = map[string][]taskTrust{
	chkAtlas.path: {
		{"deploy-preview", "Deploy a preview stack", trustChanged},
		{"db-migrate", "Apply migrations", trustOK},
		{"load-test", "Run k6 load profile", trustNone},
	},
	chkReview.path: {
		{"deploy-preview", "Deploy a preview stack", trustNone},
		{"db-migrate", "Apply migrations", trustNone},
		{"load-test", "Run k6 load profile", trustNone},
	},
}

func (t trustState) label() (tone, string) {
	switch t {
	case trustOK:
		return toneOk, "Trusted"
	case trustChanged:
		return toneAttention, "Recheck"
	}
	return toneUnknown, "Not trusted"
}

// trustSummary counts the three states, omitting zero ones.
func trustSummary(path string) string {
	n := map[trustState]int{}
	for _, t := range trustByCheckout[path] {
		n[t.state]++
	}
	var parts []string
	if n[trustOK] > 0 {
		parts = append(parts, fmt.Sprintf("%d trusted", n[trustOK]))
	}
	if n[trustChanged] > 0 {
		parts = append(parts, fmt.Sprintf("%d to recheck", n[trustChanged]))
	}
	if n[trustNone] > 0 {
		parts = append(parts, fmt.Sprintf("%d not trusted", n[trustNone]))
	}
	return strings.Join(parts, " · ")
}

// capFirst upper-cases the first letter of a sentence fragment.
func capFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
