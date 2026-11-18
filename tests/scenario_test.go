package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

var binary string

func TestMain(m *testing.M) {
	dir, e := os.MkdirTemp("", "ctxferry-tests-")
	if e != nil {
		panic(e)
	}
	binary = filepath.Join(dir, "ctxferry")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-tags", "ctxferry_test", "-o", binary, "../cmd/ctxferry")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func obj(t *testing.T, r testcli.Response, key string) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.Unmarshal(r.Data[key], &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func field(t *testing.T, r testcli.Response, key string) string {
	t.Helper()
	return obj(t, r, key)["id"].(string)
}
func failure(t *testing.T, r testcli.Result, code string) {
	t.Helper()
	v := r.JSON(t)
	if r.ExitCode == 0 || v.OK || v.Error == nil || v.Error.Code != code || r.Stderr == "" {
		t.Fatalf("expected %s: %+v %s", code, r, v.Error)
	}
}
func TestLocalLifecycle(t *testing.T) {
	h := testcli.New(t, binary)
	first := h.Run(t, "init", "--format", "json").Success(t)
	again := h.Run(t, "--format=json", "init").Success(t)
	for _, key := range []string{"workspace", "device"} {
		if field(t, first, key) != field(t, again, key) {
			t.Fatal("identity changed")
		}
	}
	if string(again.Data["created"]) != "false" {
		t.Fatal("init not idempotent")
	}
	empty := h.Run(t, "project", "list", "--format=json").Success(t)
	if string(empty.Data["projects"]) != "[]" {
		t.Fatal("empty list")
	}
	bound := h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	pid := field(t, bound, "project")
	if field(t, h.Run(t, "project", "bind", ".", "--format=json").Success(t), "project") != pid {
		t.Fatal("rebind changed identity")
	}
	for _, selector := range []string{"", pid, obj(t, bound, "project")["slug"].(string)} {
		args := []string{"project", "inspect", "--format=json"}
		if selector != "" {
			args = append(args, selector)
		}
		if field(t, h.Run(t, args...).Success(t), "project") != pid {
			t.Fatal("inspect identity")
		}
	}
	listed := h.Run(t, "project", "list", "--format=json").Success(t)
	if !strings.Contains(string(listed.Data["projects"]), pid) {
		t.Fatal("project absent")
	}
	for _, args := range [][]string{{"project", "list"}, {"project", "inspect"}, {"project", "bind", "."}, {"init"}} {
		r := h.Run(t, args...)
		if r.ExitCode != 0 || r.Stdout == "" || r.Stderr != "" {
			t.Fatalf("text output: %+v", r)
		}
	}
	entries, e := os.ReadDir(h.Dir)
	if e != nil || len(entries) != 0 {
		t.Fatal("binding wrote into checkout", entries, e)
	}
	second := testcli.New(t, binary)
	v := second.Run(t, "init", "--format=json").Success(t)
	if field(t, first, "device") == field(t, v, "device") || field(t, first, "workspace") == field(t, v, "workspace") {
		t.Fatal("device roots not isolated")
	}
	if string(second.Run(t, "project", "list", "--format=json").Success(t).Data["projects"]) != "[]" {
		t.Fatal("shared catalog")
	}
}
func TestFailuresAndJSON(t *testing.T) {
	h := testcli.New(t, binary)
	failure(t, h.Run(t, "project", "list", "--format=json"), "not-initialized")
	if _, e := os.Stat(h.DataDir); !os.IsNotExist(e) {
		t.Fatal("query created store")
	}
	h.Run(t, "init", "--format=json").Success(t)
	failure(t, h.Run(t, "project", "bind", "missing", "--format=json"), "invalid-path")
	file := filepath.Join(h.Dir, "file")
	os.WriteFile(file, []byte("x"), 0600)
	failure(t, h.Run(t, "project", "bind", file, "--format=json"), "invalid-path")
	failure(t, h.Run(t, "project", "inspect", "--format=json"), "project-unbound")
	failure(t, h.Run(t, "project", "inspect", "unknown", "--format=json"), "project-not-found")
	for _, args := range [][]string{{"init", "extra", "--format=json"}, {"--format=json", "--unknown"}, {"project", "bind", "--format=json"}, {"project", "bind", ".", "--slug=", "--format=json"}} {
		r := h.Run(t, args...)
		failure(t, r, "usage")
		if r.ExitCode != 2 {
			t.Fatal("usage exit")
		}
	}
	h.Run(t, "--format=json", "--help").Success(t)
	failure(t, h.Run(t, "--data-dir=", "init", "--format=json"), "usage")
}
func TestBindingRules(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	root := h.Dir
	for _, p := range []string{"app", "apple", "app/nested", "other/app"} {
		if e := os.MkdirAll(filepath.Join(root, p), 0700); e != nil {
			t.Fatal(e)
		}
	}
	h.Dir = filepath.Join(root, "app")
	parent := h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	h.Dir = filepath.Join(root, "app", "nested")
	if field(t, h.Run(t, "project", "inspect", "--format=json").Success(t), "project") != field(t, parent, "project") {
		t.Fatal("ancestor lookup")
	}
	nested := h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	if field(t, h.Run(t, "project", "inspect", "--format=json").Success(t), "project") != field(t, nested, "project") {
		t.Fatal("nested lookup")
	}
	h.Dir = filepath.Join(root, "apple")
	failure(t, h.Run(t, "project", "inspect", "--format=json"), "project-unbound")
	h.Dir = filepath.Join(root, "other", "app")
	other := h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	if field(t, parent, "project") == field(t, other, "project") || obj(t, parent, "project")["slug"] == obj(t, other, "project")["slug"] {
		t.Fatal("basename collision")
	}
	failure(t, h.Run(t, "project", "bind", ".", "--slug=different", "--format=json"), "binding-conflict")
	h.Dir = filepath.Join(root, "apple")
	failure(t, h.Run(t, "project", "bind", ".", "--slug=app", "--format=json"), "slug-conflict")
	if runtime.GOOS != "windows" {
		alias := filepath.Join(root, "alias")
		if e := os.Symlink(filepath.Join(root, "app"), alias); e != nil {
			t.Fatal(e)
		}
		if field(t, h.Run(t, "project", "bind", alias, "--format=json").Success(t), "project") != field(t, parent, "project") {
			t.Fatal("symlink alias")
		}
	}
	h.Dir = root
	old := h.DataDir
	h.DataDir = filepath.Join(root, "data")
	h.Run(t, "init", "--format=json").Success(t)
	failure(t, h.Run(t, "project", "bind", ".", "--format=json"), "invalid-path")
	h.DataDir = old
}
func TestConcurrentCommands(t *testing.T) {
	h := testcli.New(t, binary)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			r := h.Run(t, "init", "--format=json")
			if r.ExitCode != 0 {
				failure(t, r, "store-busy")
			} else {
				r.Success(t)
			}
		})
	}
	wg.Wait()
	h.Run(t, "init", "--format=json").Success(t)
	for range 4 {
		wg.Go(func() {
			r := h.Run(t, "project", "bind", ".", "--format=json")
			if r.ExitCode != 0 {
				failure(t, r, "store-busy")
			} else {
				r.Success(t)
			}
		})
	}
	wg.Wait()
	r := h.Run(t, "project", "list", "--format=json").Success(t)
	var list []any
	json.Unmarshal(r.Data["projects"], &list)
	if len(list) != 1 {
		t.Fatal(fmt.Sprint(list))
	}
}

func TestDamagedStoresFailClosed(t *testing.T) {
	for _, newer := range []bool{false, true} {
		t.Run(fmt.Sprint(newer), func(t *testing.T) {
			h := testcli.New(t, binary)
			h.Run(t, "init", "--format=json").Success(t)
			path := filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite")
			if newer {
				db, e := sql.Open("sqlite", path)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = db.Exec("PRAGMA user_version=99"); e != nil {
					t.Fatal(e)
				}
				db.Close()
			} else {
				if e := os.WriteFile(path, []byte("corrupt database"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			code := "store-error"
			if newer {
				code = "unsupported-schema"
			}
			failure(t, h.Run(t, "init", "--format=json"), code)
			after, e := os.ReadFile(path)
			if e != nil || string(before) != string(after) {
				t.Fatal("invalid store was modified", e)
			}
		})
	}
}

func TestFailedBindingIsAtomic(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`CREATE TRIGGER fail_binding BEFORE INSERT ON checkout_bindings BEGIN SELECT RAISE(ABORT,'injected binding failure'); END`); e != nil {
		t.Fatal(e)
	}
	failure(t, h.Run(t, "project", "bind", ".", "--format=json"), "store-error")
	if string(h.Run(t, "project", "list", "--format=json").Success(t).Data["projects"]) != "[]" {
		t.Fatal("orphan project after failed binding")
	}
}

func TestBusyIsBounded(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("BEGIN IMMEDIATE"); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("ROLLBACK")
	start := time.Now()
	failure(t, h.Run(t, "project", "bind", ".", "--format=json"), "store-busy")
	if time.Since(start) > 15*time.Second {
		t.Fatal("busy wait exceeded bound")
	}
}

func TestUUIDSelectorPrecedence(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	first := h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	id := field(t, first, "project")
	other := filepath.Join(h.Dir, "other")
	if e := os.Mkdir(other, 0700); e != nil {
		t.Fatal(e)
	}
	h.Run(t, "project", "bind", other, "--slug", id, "--format=json").Success(t)
	if field(t, h.Run(t, "project", "inspect", id, "--format=json").Success(t), "project") != id {
		t.Fatal("slug shadowed UUID")
	}
}
func TestFilesystemCaseAliases(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	path := filepath.Join(h.Dir, "Repo")
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(h.Dir, "repo")
	if _, e := os.Stat(alias); os.IsNotExist(e) {
		t.Skip("case-sensitive filesystem")
	}
	first := h.Run(t, "project", "bind", path, "--format=json").Success(t)
	if field(t, h.Run(t, "project", "bind", alias, "--format=json").Success(t), "project") != field(t, first, "project") {
		t.Fatal("case alias created duplicate")
	}
	h.Dir = alias
	if field(t, h.Run(t, "project", "inspect", "--format=json").Success(t), "project") != field(t, first, "project") {
		t.Fatal("case alias inspect failed")
	}
}
