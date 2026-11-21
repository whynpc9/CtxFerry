package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/store"
)

// Every Record kind must implement the whole seam; a partial entry would make
// the sync round silently skip that kind in one of its passes.
func TestRecordKindsAreComplete(t *testing.T) {
	ctx := context.Background()
	a := App{DataDir: filepath.Join(t.TempDir(), "data")}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seen := map[string]bool{}
	for _, k := range recordKinds {
		if k.name == "" || seen[k.name] {
			t.Fatalf("record kind %q is unnamed or registered twice", k.name)
		}
		seen[k.name] = true
		if k.validate == nil || k.install == nil || k.export == nil {
			t.Fatalf("record kind %q lacks validate, install or export", k.name)
		}
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", k.table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("record kind %q names missing projection table %q: %v", k.name, k.table, err)
		}
	}
	for _, name := range []string{"project", "repository", "membership", "context", "configuration", "resource", "task", secretBindingKind, artifactKind, skillPinKind, skillGroupKind} {
		if !seen[name] {
			t.Fatalf("published kind %q is not registered", name)
		}
	}
}

// Installation order lets a kind depend only on kinds installed before it.
func TestRecordKindsInstallDependenciesFirst(t *testing.T) {
	order := map[string]int{}
	for i, k := range recordKinds {
		order[k.name] = i
	}
	before := func(dep, kind string) {
		t.Helper()
		if order[dep] >= order[kind] {
			t.Fatalf("%s must install before %s", dep, kind)
		}
	}
	before("project", "membership")
	before("repository", "membership")
	before("membership", "task")
	before("resource", secretBindingKind)
	before(skillPinKind, skillGroupKind)
}
