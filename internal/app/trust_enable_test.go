package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/cli"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type fixture struct {
	t        *testing.T
	data     string
	services cli.Services
}

func (f fixture) run(args ...string) map[string]any {
	f.t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.RunWithServices(f.t.Context(), append([]string{"--data-dir", f.data, "--format=json"}, args...), &stdout, &stderr, f.services)
	var out struct {
		OK    bool
		Data  map[string]any
		Error *app.Error
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil || code != 0 || !out.OK {
		f.t.Fatalf("%v failed: %d %s %s", args, code, stdout.String(), stderr.String())
	}
	return out.Data
}

// enableFixture registers a credentialed Task through the CLI in one checkout.
func enableFixture(t *testing.T) (fixture, app.App) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git unavailable:", string(out))
		}
	}
	t.Chdir(repo)
	fake := &providertest.Fake{Methods: []provider.MethodCapabilities{{Method: "machine", MachineAuth: true}}}
	providers := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	f := fixture{t, filepath.Join(t.TempDir(), "data"), cli.Services{Providers: providers}}
	f.run("init")
	membership := f.run("project", "bind", ".")["binding"].(map[string]any)["membershipId"].(string)
	f.run("profile", "create", "dev")
	resource := f.run("resource", "add", "--identity", "db", "--kind", "database", "--stage", "dev", "--purpose", "Test", "--storage", "shared")["id"].(string)
	f.run("task", "add", "check", "--purpose", "Check", "--membership", membership, "--program", "check", "--arg=one", "--storage", "shared",
		"--resource-slot", `{"name":"db","kind":"database","stage":"dev"}`,
		"--credential-requirement", `{"name":"login","resourceSlot":"db","purpose":"runtime","type":"token","env":"TOKEN"}`)
	f.run("profile", "map-resource", "dev", "check", "db", resource)
	f.run("profile", "select", "dev")
	f.run("secret", "bind", "check", "login", "--profile", "dev", "--alias", "x", "--provider", "fake", "--provider-profile", "team",
		"--method", "machine", "--backend-project", "p", "--environment", "dev", "--path", "/", "--key", "K")
	return f, app.App{DataDir: f.data, Providers: providers}
}

// An edit committed after discovery but before the write lock is detected
// under the lock: enable reports preview-stale and stores nothing.
func TestEnableRejectsChangeAfterDiscovery(t *testing.T) {
	f, a := enableFixture(t)
	preview := f.run("task", "trust", "preview", "check")["preview"].(string)
	revision := f.run("task", "inspect", "check")["revision"].(string)
	argv := []string{"two"}
	defer app.SetEnableHooks(func() {
		if _, err := a.UpdateTask(t.Context(), "", "", "check", app.TaskChange{IfRevision: revision, Argv: &argv}); err != nil {
			t.Error(err)
		}
	}, func() {})()
	_, err := a.EnableExecution(t.Context(), "", ".", "check", "", "", preview)
	if app.Classify(err).Code != "preview-stale" {
		t.Fatal("an obsolete definition was enabled", err)
	}
	if state := f.run("task", "trust", "preview", "check")["state"]; state != "not-enabled" {
		t.Fatal("an approval was stored", state)
	}
}

// Preparation under the lock blocks every other writer until the approval is
// stored, and runs no Git discovery.
func TestEnableHoldsWriteLock(t *testing.T) {
	f, a := enableFixture(t)
	preview := f.run("task", "trust", "preview", "check")["preview"].(string)
	var concurrent error
	locked := false
	defer app.SetEnableHooks(func() {}, func() {
		locked = true
		path := filepath.ToSlash(store.Path(f.data))
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		db, err := sql.Open("sqlite", "file://"+path+"?_pragma=busy_timeout(50)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, concurrent = db.Exec("BEGIN IMMEDIATE"); concurrent == nil {
			db.Exec("ROLLBACK")
		}
		// Git is unavailable under the lock: a pinned checkout needs none.
		t.Setenv("PATH", t.TempDir())
	})()
	review, err := a.EnableExecution(t.Context(), "", ".", "check", "", "", preview)
	if err != nil || review.State != "enabled" {
		t.Fatal(err)
	}
	if !locked || concurrent == nil {
		t.Fatal("a concurrent writer could commit while enable prepared the definition")
	}
}
