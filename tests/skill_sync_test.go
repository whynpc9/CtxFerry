package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func addSharedSkillFixture(t *testing.T, h *testcli.Harness, mode, name, body string) string {
	t.Helper()
	src := t.TempDir()
	writeSkillFixture(t, filepath.Join(src, "SKILL.md"), skillFixtureBody(name, body), 0600)
	args := []string{"skill", "add", "--local", src, "--format=json"}
	if mode != "" {
		args = append(args, "--share", mode)
	}
	h.Run(t, args...).Success(t)
	return src
}

func skillFixtureBody(name, body string) string {
	return "---\nname: " + name + "\n---\n" + body + "\n"
}

func planSkillGroup(t *testing.T, h *testcli.Harness, selection string) app.SkillGroupPlan {
	t.Helper()
	result := h.Run(t, "skill", "plan", "--group", selection, "--target", "codex", "--format=json")
	result.Success(t)
	var envelope struct{ Data app.SkillGroupPlan }
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func usePublicSkillFixture(t *testing.T, device, source *testcli.Harness, url string) {
	t.Helper()
	// Rewrite only this synthetic HTTPS source for this device's Git. Sync
	// uses a separate local bare repository via the test transport.
	git(t, device, "config", "--file", filepath.Join(device.Home, ".gitconfig"),
		"url."+filepath.ToSlash(source.Dir)+".insteadOf", url)
}

func TestSharedSkillsAcrossDevices(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	public := gitHarness(t)
	url := "https://public-skill-fixture.invalid/skills.git"
	usePublicSkillFixture(t, a, public, url)
	publicPath := filepath.Join(public.Dir, "selected", "SKILL.md")
	pinned := skillFixtureBody("public-fixture", "public pin v1")
	writeSkillFixture(t, publicPath, pinned, 0600)
	git(t, public, "add", ".")
	git(t, public, "commit", "-m", "public v1")
	git(t, public, "tag", "release")
	pin := git(t, public, "rev-parse", "HEAD")
	a.Run(t, "skill", "add", "--share", "public", "--git", url, "--subdir", "selected", "--ref", "refs/tags/release", "--format=json").Success(t)
	git(t, a, "config", "--file", filepath.Join(a.Home, ".gitconfig"),
		"--unset", "url."+filepath.ToSlash(public.Dir)+".insteadOf")
	writeSkillFixture(t, publicPath, skillFixtureBody("public-fixture", "moving public ref"), 0600)
	git(t, public, "commit", "-am", "public v2")
	git(t, public, "tag", "-f", "release")

	privateSource := t.TempDir()
	privateManifest := skillFixtureBody("private-fixture", "private shared payload")
	writeSkillFixture(t, filepath.Join(privateSource, "SKILL.md"), privateManifest, 0600)
	writeSkillFixture(t, filepath.Join(privateSource, "assets", "empty"), "", 0600)
	a.Run(t, "skill", "add", "--share", "private", "--local", privateSource, "--format=json").Success(t)
	localSource := addSharedSkillFixture(t, a, "", "local-fixture", "local-only Skill sentinel")
	failure(t, a.Run(t, "skill", "group", "set", "global", "local-fixture", "--format=json"), "invalid-reference")
	for selection, names := range map[string][]string{
		"global":            {"public-fixture"},
		"project:checkout":  {"private-fixture"},
		"capability:review": {"public-fixture", "private-fixture"},
	} {
		args := append([]string{"skill", "group", "set", selection, "--format=json"}, names...)
		a.Run(t, args...).Success(t)
	}
	apply := []string{"skill", "apply", "private-fixture", "--target", "claude-code", "--format=json"}
	if runtime.GOOS != "windows" {
		apply = append(apply, "--symlink")
	}
	installedA := skillPlan(t, a.Run(t, apply...))
	if installedA.Status != "installed" {
		t.Fatal(installedA)
	}
	sharedArtifact := addSyncArtifact(t, a, "same-object.json", privateManifest, "internal")
	explicitBody := strings.Repeat("a", app.BodyLimit+1)
	explicitArtifact := addSyncArtifact(t, a, "explicit-sample.json", explicitBody, "internal")
	enableSync(t, a)
	queueArtifact(t, a, sharedArtifact.ID, "automatic")
	queueArtifact(t, a, explicitArtifact.ID, "explicit")
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	tree := git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1")
	if strings.Count(tree, "objects/"+sharedArtifact.Payload.SHA256) != 1 {
		t.Fatal("the private Skill and artifact must reference the same published object")
	}
	for path := range strings.SplitSeq(tree, "\n") {
		data := git(t, a, "--git-dir", remote, "show", "ctxferry-v1:"+path)
		for _, forbidden := range []string{privateSource, localSource, a.Home, a.DataDir, installedA.Path, "local-only Skill sentinel", "local-fixture", "public pin v1"} {
			if strings.Contains(data, forbidden) || strings.Contains(data, filepath.ToSlash(forbidden)) {
				t.Fatalf("published %q includes %q", path, forbidden)
			}
		}
	}

	skillPlan(t, a.Run(t, "skill", "apply", "public-fixture", "--target", "codex", "--format=json"))
	b := joinDevice(t, a, false)
	failure(t, b.Run(t, "skill", "plan", "--group", "project:checkout", "--target", "codex", "--format=json"), "project-unsubscribed")
	b.Run(t, "project", "subscribe", "checkout", "--format=json").Success(t)
	fetchSyncedArtifact(t, b, sharedArtifact.ID, privateManifest)
	fetchSyncedArtifact(t, b, explicitArtifact.ID, explicitBody)
	failure(t, b.Run(t, "skill", "plan", "local-fixture", "--target", "codex", "--format=json"), "skill-not-found")
	for selection, names := range map[string][]string{
		"global":            {"public-fixture"},
		"project:checkout":  {"private-fixture"},
		"capability:review": {"private-fixture", "public-fixture"},
	} {
		before := skillPaths(t, b.DataDir)
		plan := planSkillGroup(t, b, selection)
		if plan.Group.Name != selection || !slices.Equal(plan.Group.Skills, names) || len(plan.Plans) != len(names) {
			t.Fatal(plan)
		}
		for i, p := range plan.Plans {
			if p.Status != "ready" || p.Package.Name != names[i] || p.Path != filepath.Join(skillHome(t, b), ".agents", "skills", names[i]) {
				t.Fatal(p)
			}
			if _, err := os.Lstat(p.Path); !os.IsNotExist(err) {
				t.Fatalf("plan projected %s: %v", p.Path, err)
			}
		}
		if after := skillPaths(t, b.DataDir); !slices.Equal(after, before) {
			t.Fatalf("plan changed storage paths: %v → %v", before, after)
		}
	}
	usePublicSkillFixture(t, b, public, url)
	for _, name := range []string{"public-fixture", "private-fixture"} {
		p := skillPlan(t, b.Run(t, "skill", "apply", name, "--target", "codex", "--format=json"))
		got, err := os.ReadFile(filepath.Join(p.Path, "SKILL.md"))
		want := pinned
		if name == "private-fixture" {
			want = privateManifest
			if empty, err := os.ReadFile(filepath.Join(p.Path, "assets", "empty")); err != nil || len(empty) != 0 {
				t.Fatalf("private empty file: %q %v", empty, err)
			}
			if p.Package.SourceURL != "" {
				t.Fatal(p.Package)
			}
		} else if p.Package.Commit != pin {
			t.Fatalf("installed %s, want %s", p.Package.Commit, pin)
		}
		if p.Status != "installed" || err != nil || string(got) != want {
			t.Fatalf("applied %s: %q, %v", name, got, err)
		}
	}
	git(t, b, "config", "--file", filepath.Join(b.Home, ".gitconfig"),
		"--unset", "url."+filepath.ToSlash(public.Dir)+".insteadOf")
	before := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if after := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); after != before {
		t.Fatalf("local install changed shared history: %s → %s", before, after)
	}
}

func TestSharedSkillPinConflictHoldsDependentGroup(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	addSharedSkillFixture(t, a, "private", "changing", "base pin")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)

	installed := skillPlan(t, a.Run(t, "skill", "apply", "changing", "--target", "claude-code", "--format=json"))
	addSharedSkillFixture(t, a, "private", "changing", "remote pin")
	a.Run(t, "sync", "--format=json").Success(t)
	next := skillPlan(t, a.Run(t, "skill", "plan", "changing", "--target", "claude-code", "--format=json"))
	if next.Status != "blocked" || next.Issues[0].Code != "skill-pin-changed" {
		t.Fatal(next)
	}
	failure(t, a.Run(t, "skill", "apply", "changing", "--target", "claude-code", "--format=json"), "skill-apply-blocked")
	if data, err := os.ReadFile(filepath.Join(installed.Path, "SKILL.md")); err != nil || string(data) != skillFixtureBody("changing", "base pin") {
		t.Fatalf("the new shared pin altered an existing install: %q %v", data, err)
	}
	addSharedSkillFixture(t, b, "private", "changing", "local pin")
	b.Run(t, "skill", "group", "set", "capability:review", "changing", "--format=json").Success(t)
	addSharedSkillFixture(t, b, "private", "independent", "independent pin")
	b.Run(t, "sync", "--format=json").Success(t)

	status := b.Run(t, "sync", "status", "--format=json").Success(t)
	var conflicts []app.ConflictInfo
	if err := json.Unmarshal(status.Data["conflicts"], &conflicts); err != nil || len(conflicts) != 1 {
		t.Fatalf("expected one pin conflict: %s (%v)", status.Data["conflicts"], err)
	}
	conflict := conflicts[0]
	var blocked []app.BlockedRecord
	if err := json.Unmarshal(status.Data["blocked"], &blocked); err != nil ||
		!slices.ContainsFunc(blocked, func(r app.BlockedRecord) bool { return r.Reason == "dependency:"+conflict.ID }) {
		t.Fatalf("expected group blocked on pin: %s (%v)", status.Data["blocked"], err)
	}
	inspect := b.Run(t, "sync", "conflicts", conflict.ID, "--format=json").Success(t)
	if obj(t, inspect, "local")["kind"] != "skill-pin" {
		t.Fatal(inspect)
	}
	failure(t, b.Run(t, "skill", "plan", "--group", "capability:review", "--target", "codex", "--format=json"), "conflict-unresolved")
	failure(t, b.Run(t, "skill", "apply", "changing", "--target", "codex", "--format=json"), "conflict-unresolved")
	failure(t, b.Run(t, "sync", "resolve", conflict.ID, "--if-conflict", "stale", "--take", "local", "--format=json"), "stale-conflict")

	a.Run(t, "sync", "--format=json").Success(t)
	failure(t, a.Run(t, "skill", "plan", "--group", "capability:review", "--target", "codex", "--format=json"), "skill-group-not-found")
	a.Run(t, "skill", "plan", "independent", "--target", "codex", "--format=json").Success(t)
	before := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "resolve", conflict.ID, "--if-conflict", conflict.ConflictID, "--take", "local", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	if after := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1^"); before != after {
		t.Fatalf("choice and dependent selection did not publish together: %s != %s", before, after)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	if p := planSkillGroup(t, a, "capability:review"); !slices.Equal(p.Group.Skills, []string{"changing"}) {
		t.Fatal(p)
	}
	p := skillPlan(t, a.Run(t, "skill", "apply", "changing", "--target", "codex", "--format=json"))
	data, err := os.ReadFile(filepath.Join(p.Path, "SKILL.md"))
	if err != nil || string(data) != skillFixtureBody("changing", "local pin") {
		t.Fatalf("resolved pin was not installed: %q %v", data, err)
	}

	// An existing install needs an explicit update and can roll back locally.
	for _, change := range []struct{ action, want string }{{"update", "local pin"}, {"rollback", "base pin"}} {
		args := []string{"skill", change.action, "changing", "--target", "claude-code", "--format=json"}
		preview := a.Run(t, args...).Success(t)
		var token string
		if err := json.Unmarshal(preview.Data["confirmation"], &token); err != nil || token == "" {
			t.Fatalf("expected confirmation for %s: %s (%v)", change.action, preview.Data["confirmation"], err)
		}
		a.Run(t, append(args, "--confirm", token)...).Success(t)
		got, err := os.ReadFile(filepath.Join(installed.Path, "SKILL.md"))
		if err != nil || string(got) != skillFixtureBody("changing", change.want) {
			t.Fatalf("%s of installed shared Skill: %q %v", change.action, got, err)
		}
	}
}

func TestSharedPrivateSkillRejectsIncompletePackage(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	addSharedSkillFixture(t, a, "private", "private-base", "initial private payload")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	before := b.Run(t, "sync", "status", "--format=json").Success(t)

	tooBig := t.TempDir()
	writeSkillFixture(t, filepath.Join(tooBig, "SKILL.md"), skillFixtureBody("too-big", "oversize"), 0600)
	writeSkillFixture(t, filepath.Join(tooBig, "data"), strings.Repeat("x", app.BodyLimit), 0600)
	failure(t, a.Run(t, "skill", "add", "--share", "private", "--local", tooBig, "--format=json"), "size-limit")
	failure(t, a.Run(t, "skill", "plan", "too-big", "--target", "codex", "--format=json"), "skill-not-found")

	addSharedSkillFixture(t, a, "private", "private-incoming", "incoming private payload")
	a.Run(t, "sync", "--format=json").Success(t)
	checkout := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, checkout)
	object := git(t, a, "-C", checkout, "diff", "--name-only", "HEAD^", "HEAD", "--", "objects/")
	if !strings.HasPrefix(object, "objects/") || strings.Contains(object, "\n") {
		t.Fatalf("expected one new object, got %q", object)
	}
	git(t, a, "-C", checkout, "rm", "--", object)
	git(t, a, "-C", checkout, "commit", "-m", "omit referenced Skill file")
	git(t, a, "-C", checkout, "push", "origin", "ctxferry-v1")
	failure(t, b.Run(t, "sync", "--format=json"), "incomplete")
	after := b.Run(t, "sync", "status", "--format=json").Success(t)
	if string(after.Data["head"]) != string(before.Data["head"]) {
		t.Fatal("incomplete package advanced the confirmed sync head")
	}
	failure(t, b.Run(t, "skill", "plan", "private-incoming", "--target", "codex", "--format=json"), "skill-not-found")
	b.Run(t, "skill", "apply", "private-base", "--target", "codex", "--format=json").Success(t)
}

func TestSharedSkillAndAgentBootstrapKeepSeparateOwnership(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	const body = "explicitly shared package"
	addSharedSkillFixture(t, h, "private", "ctxferry", body)

	preview := bootstrapPlan(t, bootstrapRun(t, h, "codex", "install"))
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "install", "--preview", preview.Preview))
	failure(t, h.Run(t, "skill", "apply", "ctxferry", "--target", "codex", "--format=json"), "skill-apply-blocked")
	assertBootstrapFile(t, preview.Writes[0].Path, preview.Writes[0].Content)

	installed := skillPlan(t, h.Run(t, "skill", "apply", "ctxferry", "--target", "claude-code", "--format=json"))
	failure(t, bootstrapRun(t, h, "claude-code", "install"), "skill-name-collision")
	assertBootstrapFile(t, filepath.Join(installed.Path, "SKILL.md"), skillFixtureBody("ctxferry", body))
}
