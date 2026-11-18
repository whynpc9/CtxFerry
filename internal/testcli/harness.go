// Package testcli provides the executable-level seam shared by CLI scenarios.
package testcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type Harness struct {
	Binary, DataDir, Dir, Home string
	GitPath                    string
	ExtraEnv                   []string
}
type Result struct {
	Stdout, Stderr string
	ExitCode       int
}
type Response struct {
	SchemaVersion int                             `json:"schemaVersion"`
	OK            bool                            `json:"ok"`
	Data          map[string]json.RawMessage      `json:"data"`
	Error         *struct{ Code, Message string } `json:"error"`
}

func New(t *testing.T, binary string) *Harness {
	t.Helper()
	root := t.TempDir()
	h := &Harness{Binary: binary, DataDir: filepath.Join(root, "data"), Dir: filepath.Join(root, "checkout"), Home: filepath.Join(root, "home")}
	for _, p := range []string{h.Dir, h.Home} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	return h
}
func (h *Harness) Run(t *testing.T, args ...string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.Binary, append([]string{"--data-dir", h.DataDir}, args...)...)
	cmd.Dir = h.Dir
	// Deliberately exclude credentials and GUI/session variables.
	cmd.Env = []string{"HOME=" + h.Home, "USERPROFILE=" + h.Home, "LOCALAPPDATA=" + h.Home, "PATH=" + filepath.Dir(h.Binary), "GOPROXY=off"}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	if h.GitPath != "" {
		cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(h.Binary)+string(os.PathListSeparator)+h.GitPath)
	}
	cmd.Env = append(cmd.Env, h.ExtraEnv...)
	var out, diagnostics bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	e := cmd.Run()
	code := 0
	if e != nil {
		var exit *exec.ExitError
		if !errors.As(e, &exit) {
			t.Fatal(e)
		}
		code = exit.ExitCode()
	}
	return Result{out.String(), diagnostics.String(), code}
}
func (r Result) JSON(t *testing.T) Response {
	t.Helper()
	var v Response
	d := json.NewDecoder(bytes.NewBufferString(r.Stdout))
	if e := d.Decode(&v); e != nil {
		t.Fatalf("invalid JSON: %v: %s stderr=%s", e, r.Stdout, r.Stderr)
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		t.Fatalf("extra stdout: %s", r.Stdout)
	}
	if v.SchemaVersion != 1 {
		t.Fatalf("schema: %+v", v)
	}
	return v
}
func (r Result) Success(t *testing.T) Response {
	t.Helper()
	v := r.JSON(t)
	if r.ExitCode != 0 || !v.OK || r.Stderr != "" {
		t.Fatalf("command failed: %+v", r)
	}
	return v
}
