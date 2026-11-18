package tests

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func git(t *testing.T, h *testcli.Harness, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = h.Dir
	cmd.Env = append(os.Environ(), "HOME="+h.Home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
	return strings.TrimSpace(string(out))
}
func gitHarness(t *testing.T) *testcli.Harness {
	t.Helper()
	path, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	h := testcli.New(t, binary)
	h.GitPath = filepath.Dir(path)
	h.Run(t, "init", "--format=json").Success(t)
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "initial")
	return h
}
func bind(t *testing.T, h *testcli.Harness, args ...string) testcli.Response {
	t.Helper()
	return h.Run(t, append([]string{"project", "bind", ".", "--format=json"}, args...)...).Success(t)
}
func inspect(t *testing.T, h *testcli.Harness) testcli.Response {
	t.Helper()
	return h.Run(t, "project", "inspect", "--format=json").Success(t)
}
func unbound(t *testing.T, h *testcli.Harness) map[string]any {
	t.Helper()
	r := h.Run(t, "project", "inspect", "--format=json")
	failure(t, r, "project-unbound")
	var v struct {
		Error struct{ Details map[string]any }
	}
	if e := json.Unmarshal([]byte(r.Stdout), &v); e != nil {
		t.Fatal(e)
	}
	return v.Error.Details
}
func TestWorktreesAndMemberships(t *testing.T) {
	h := gitHarness(t)
	root := h.Dir
	for _, p := range []string{"services/api/admin", "web", "other"} {
		if e := os.MkdirAll(filepath.Join(root, p), 0700); e != nil {
			t.Fatal(e)
		}
	}
	h.Dir = filepath.Join(root, "services/api")
	api := bind(t, h, "--slug=api")
	h.Dir = filepath.Join(root, "services/api/admin")
	admin := bind(t, h, "--slug=admin")
	h.Dir = filepath.Join(root, "web")
	web := bind(t, h, "--slug=web")
	repo := obj(t, api, "binding")["repositoryId"].(string)
	if obj(t, web, "binding")["repositoryId"] != repo {
		t.Fatal("common-dir repository not reused")
	}
	h.Dir = root
	wt := filepath.Join(filepath.Dir(root), "worktree")
	git(t, h, "worktree", "add", "-b", "second", wt)
	for _, p := range []string{"services/api/admin", "web", "other"} {
		os.MkdirAll(filepath.Join(wt, p), 0700)
	}
	for p, want := range map[string]string{"services/api": field(t, api, "project"), "services/api/admin": field(t, admin, "project"), "web": field(t, web, "project")} {
		h.Dir = filepath.Join(wt, p)
		r := inspect(t, h)
		if field(t, r, "project") != want {
			t.Fatal("scope", p)
		}
		res := obj(t, r, "resolution")
		// The resolver returns canonical paths: macOS /var and /private/var,
		// and Windows drive casing, can name the same directory differently.
		current, err := os.Stat(res["git"].(map[string]any)["checkoutRoot"].(string))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.Stat(wt)
		if err != nil {
			t.Fatal(err)
		}
		if res["source"] != "git-common-dir" || !os.SameFile(current, expected) {
			t.Fatal(res)
		}
	}
	h.Dir = wt
	r := unbound(t, h)
	if len(r["candidates"].([]any)) != 3 {
		t.Fatal(r)
	}
	h.Dir = filepath.Join(wt, "other")
	unbound(t, h)
	// Explicit Project overrides every detection stage, even another path binding.
	h.Dir = filepath.Join(wt, "web")
	r2 := h.Run(t, "project", "inspect", "--project=api", "--format=json").Success(t)
	if field(t, r2, "project") != field(t, api, "project") || obj(t, r2, "resolution")["source"] != "explicit" {
		t.Fatal(r2)
	}
	failure(t, h.Run(t, "project", "inspect", "--project=missing", "--format=json"), "project-not-found")
	h.Dir = wt
	bind(t, h, "--project=api")
	h.Dir = filepath.Join(wt, "web")
	if field(t, inspect(t, h), "project") != field(t, api, "project") {
		t.Fatal("path must win")
	}
	// Another repository can be an explicit membership of the same Project.
	second := filepath.Join(filepath.Dir(root), "second-repo")
	os.Mkdir(second, 0700)
	h.Dir = second
	git(t, h, "init")
	b := bind(t, h, "--project=api")
	if field(t, b, "project") != field(t, api, "project") || obj(t, b, "binding")["repositoryId"] == repo {
		t.Fatal("multi-repository membership")
	}
}
func TestRemoteCandidatesAndAliases(t *testing.T) {
	h := gitHarness(t)
	bound := bind(t, h, "--slug=api", "--remote=git@github.com:Owner/Repo.git")
	repo := obj(t, bound, "binding")["repositoryId"].(string)
	var bindings []map[string]any
	if e := json.Unmarshal(inspect(t, h).Data["bindings"], &bindings); e != nil {
		t.Fatal(e)
	}
	if len(bindings) != 1 || bindings[0]["repositoryId"] != repo || bindings[0]["membershipId"] != obj(t, bound, "binding")["membershipId"] {
		t.Fatal("inspection lost persisted identity", bindings)
	}
	textBound := h.Run(t, "project", "bind", ".")
	if !strings.Contains(textBound.Stdout, repo) {
		t.Fatal(textBound)
	}
	textInspect := h.Run(t, "project", "inspect")
	if !strings.Contains(textInspect.Stdout, repo) {
		t.Fatal(textInspect)
	}
	root := h.Dir
	clone := filepath.Join(filepath.Dir(root), "clone")
	os.Mkdir(clone, 0700)
	h.Dir = clone
	git(t, h, "init")
	for _, remote := range []string{"https://github.com/owner/repo", "https://github.com/owner/repo.git", "ssh://git@github.com/Owner/Repo.git", "git@github.com:owner/repo.git"} {
		git(t, h, "config", "remote.origin.url", remote)
		r := unbound(t, h)
		c := r["candidates"].([]any)
		if len(c) != 1 || c[0].(map[string]any)["project"].(map[string]any)["id"] != field(t, bound, "project") {
			t.Fatal(r)
		}
	}
	git(t, h, "config", "remote.origin.url", "https://github.com/fork/repo.git")
	if len(unbound(t, h)["candidates"].([]any)) != 0 {
		t.Fatal("fork inherited identity")
	}
	git(t, h, "remote", "add", "upstream", "https://github.com/owner/repo.git")
	if len(unbound(t, h)["candidates"].([]any)) != 1 {
		t.Fatal("upstream candidate")
	}
	text := h.Run(t, "project", "inspect")
	if text.ExitCode != 1 || !strings.Contains(text.Stderr, "Candidate: api") {
		t.Fatal(text)
	}
	h.Run(t, "project", "remote", "add", repo, "https://github.com/owner/renamed.git", "--format=json").Success(t)
	git(t, h, "remote", "remove", "upstream")
	for _, name := range []string{"repo", "renamed"} {
		git(t, h, "config", "remote.origin.url", "https://github.com/owner/"+name)
		if len(unbound(t, h)["candidates"].([]any)) != 1 {
			t.Fatal("alias lost")
		}
	}
	failure(t, h.Run(t, "project", "remote", "add", repo, "https://token@github.com/owner/private.git", "--format=json"), "invalid-remote")
	// Explicitly registering the same alias on a different Repository represents ambiguity.
	other := bind(t, h, "--slug=other", "--remote=https://github.com/owner/repo.git")
	if field(t, other, "project") == field(t, bound, "project") {
		t.Fatal("auto binding")
	}
	third := filepath.Join(filepath.Dir(root), "third")
	os.Mkdir(third, 0700)
	h.Dir = third
	git(t, h, "init")
	git(t, h, "remote", "add", "origin", "https://github.com/owner/repo.git")
	if len(unbound(t, h)["candidates"].([]any)) != 2 {
		t.Fatal("ambiguity hidden")
	}
	// Pure reads do not register the new common directory or a path binding.
	if len(unbound(t, h)["candidates"].([]any)) != 2 {
		t.Fatal("inspect mutated state")
	}
	h.Dir = root
	git(t, h, "remote", "add", "origin", "https://github.com/owner/repo.git")
	if field(t, inspect(t, h), "project") != field(t, bound, "project") {
		t.Fatal("path precedence")
	}
}
func TestUnavailableGitAndNonGit(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	b := bind(t, h)
	r := inspect(t, h)
	res := obj(t, r, "resolution")
	if res["git"].(map[string]any)["status"] != "unavailable" || res["device"].(map[string]any)["id"] == "" {
		t.Fatal(res)
	}
	if field(t, r, "project") != field(t, b, "project") {
		t.Fatal(r)
	}
	path, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	h.GitPath = filepath.Dir(path)
	if obj(t, inspect(t, h), "resolution")["git"].(map[string]any)["status"] != "not-git" {
		t.Fatal("non-git")
	}
}
func TestGitEnvironmentCannotRedirectDiscovery(t *testing.T) {
	h := gitHarness(t)
	b := bind(t, h)
	root := h.Dir
	other := filepath.Join(filepath.Dir(root), "unrelated")
	os.Mkdir(other, 0700)
	h.Dir = other
	h.ExtraEnv = []string{"GIT_DIR=" + filepath.Join(root, ".git"), "GIT_WORK_TREE=" + root}
	r := unbound(t, h)
	if r["git"].(map[string]any)["status"] != "not-git" {
		t.Fatal(r)
	}
	h.Run(t, "project", "inspect", "--project="+field(t, b, "project"), "--format=json").Success(t)
}

func TestEqualScopeCommonDirAmbiguity(t *testing.T) {
	h := gitHarness(t)
	root := h.Dir
	bind(t, h, "--slug=first")
	wt := filepath.Join(filepath.Dir(root), "wt")
	git(t, h, "worktree", "add", "-b", "second", wt)
	h.Dir = wt
	bind(t, h, "--slug=second")
	h.Dir = root
	third := filepath.Join(filepath.Dir(root), "third")
	git(t, h, "worktree", "add", "-b", "third", third)
	h.Dir = third
	if len(unbound(t, h)["candidates"].([]any)) != 2 {
		t.Fatal("equal scopes must be ambiguous")
	}
}

func TestRootAndSingleCharacterScope(t *testing.T) {
	h := gitHarness(t)
	root := h.Dir
	bind(t, h, "--slug=root")
	os.Mkdir(filepath.Join(root, "a"), 0700)
	h.Dir = filepath.Join(root, "a")
	b := bind(t, h, "--slug=nested")
	h.Dir = root
	wt := filepath.Join(filepath.Dir(root), "wt")
	git(t, h, "worktree", "add", "-b", "wt", wt)
	os.Mkdir(filepath.Join(wt, "a"), 0700)
	h.Dir = filepath.Join(wt, "a")
	if field(t, inspect(t, h), "project") != field(t, b, "project") {
		t.Fatal("root shadowed one-character scope")
	}
}

func TestGitRegistrationRollsBack(t *testing.T) {
	h := gitHarness(t)
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`CREATE TRIGGER fail_alias BEFORE INSERT ON repository_aliases BEGIN SELECT RAISE(ABORT,'injected alias failure'); END`); e != nil {
		t.Fatal(e)
	}
	failure(t, h.Run(t, "project", "bind", ".", "--remote=https://github.com/owner/repo", "--format=json"), "store-error")
	for _, table := range []string{"projects", "checkout_bindings", "repositories", "memberships", "repository_aliases"} {
		var n int
		if e = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
}
func TestForeignDeviceBindingsAreIgnored(t *testing.T) {
	h := gitHarness(t)
	b := bind(t, h)
	root := h.Dir
	wt := filepath.Join(filepath.Dir(root), "wt")
	git(t, h, "worktree", "add", "-b", "wt", wt)
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("UPDATE checkout_bindings SET device_id='foreign'"); e != nil {
		t.Fatal(e)
	}
	h.Dir = wt
	unbound(t, h)
	h.Dir = root
	unbound(t, h)
	if field(t, h.Run(t, "project", "inspect", "--project="+field(t, b, "project"), "--format=json").Success(t), "project") != field(t, b, "project") {
		t.Fatal("explicit selector failed")
	}
}
