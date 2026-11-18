package platform

import (
	"path/filepath"
	"testing"
)

func TestOverrides(t *testing.T) {
	env := t.TempDir()
	t.Setenv("CTXFERRY_DATA_DIR", env)
	if got, e := DataDir("", false); e != nil || got != env {
		t.Fatal(got, e)
	}
	flag := t.TempDir()
	if got, e := DataDir(flag, true); e != nil || got != flag {
		t.Fatal(got, e)
	}
	if _, e := DataDir("", true); e == nil {
		t.Fatal("empty override accepted")
	}
	got, e := DataDir("relative", true)
	if e != nil || !filepath.IsAbs(got) {
		t.Fatal(got, e)
	}
}
