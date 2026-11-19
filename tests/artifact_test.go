package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func artifactData[T any](t *testing.T, r testcli.Response) T {
	t.Helper()
	wire, err := json.Marshal(r.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(wire, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func artifactAddArgs(file string) []string {
	return []string{"artifact", "add", file, "--format=json", "--type=application/json", "--sensitivity=internal",
		"--source-revision=source-rev", "--dirty=true", "--diff-id=diff-42", "--profile=dev",
		"--tool=go@1.27.4", "--input=dataset:v3", "--input=model:abc", "--command=go", "--command=test",
		"--command=-count=1", "--exit-code=1", "--summary=One test failed", "--completed=unit", "--uncompleted=integration"}
}

func writeArtifactFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactLifecycle(t *testing.T) {
	h := testcli.New(t, binary)
	device := field(t, h.Run(t, "init", "--format=json").Success(t), "device")
	project := field(t, h.Run(t, "project", "bind", ".", "--format=json").Success(t), "project")
	const name = "report.json"
	const content = `{"passed":false}`
	writeArtifactFile(t, filepath.Join(h.Dir, name), content)
	writeArtifactFile(t, filepath.Join(h.Dir, "unregistered.json"), "{}")

	list := func(args ...string) []app.ArtifactManifest {
		t.Helper()
		command := append([]string{"artifact", "list", "--format=json"}, args...)
		return artifactData[app.ArtifactList](t, h.Run(t, command...).Success(t)).Artifacts
	}
	if manifests := list(); manifests == nil || len(manifests) != 0 {
		t.Fatalf("unregistered files were listed: %+v", manifests)
	}
	failure(t, h.Run(t, artifactAddArgs(".")...), "invalid-path")
	add := func() app.ArtifactManifest {
		t.Helper()
		return artifactData[app.ArtifactManifest](t, h.Run(t, artifactAddArgs(name)...).Success(t))
	}
	first := add()
	r := first.Run
	if first.ID == "" || r.ID == "" || r.ProjectID != project || r.DeviceID != device || r.OS != runtime.GOOS || r.Arch != runtime.GOARCH {
		t.Fatalf("missing identity: %+v", first)
	}
	for _, stamp := range []string{r.CreatedAt, first.CreatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			t.Fatalf("invalid creation time %q: %v", stamp, err)
		}
	}
	want := app.RunProvenance{
		SourceRevision: "source-rev", Dirty: true, DiffID: "diff-42", Profile: "dev",
		ToolVersions: []string{"go@1.27.4"}, Inputs: []string{"dataset:v3", "model:abc"},
		Command: []string{"go", "test", "-count=1"}, ExitCode: 1, Summary: "One test failed",
		Completed: []string{"unit"}, Uncompleted: []string{"integration"},
	}
	if !reflect.DeepEqual(r.RunProvenance, want) {
		t.Fatalf("provenance: got %+v, want %+v", r.RunProvenance, want)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	p := first.Payload
	if p.Name != name || p.Type != "application/json" || p.Size != int64(len(content)) || p.SHA256 != hash ||
		p.Sensitivity != "internal" || p.Storage != "local-only" || p.Location != "objects/sha256/"+hash {
		t.Fatalf("payload: %+v", p)
	}
	cached := filepath.Join(h.DataDir, "workspaces", "default", "artifacts", filepath.FromSlash(p.Location))
	if err := os.Remove(filepath.Join(h.Dir, name)); err != nil {
		t.Fatal(err)
	}
	if got := artifactData[app.ArtifactManifest](t, h.Run(t, "artifact", "inspect", first.ID, "--format=json").Success(t)); !reflect.DeepEqual(got, first) {
		t.Fatalf("inspect changed the manifest: %+v", got)
	}
	fetch := func(path string) testcli.Result {
		t.Helper()
		return h.Run(t, "artifact", "fetch", first.ID, "--output", path, "--format=json")
	}
	out := filepath.Join(h.Dir, "retrieved.json")
	receipt := artifactData[app.ArtifactFetch](t, fetch(out).Success(t))
	if receipt.ID != first.ID || receipt.Size != p.Size || receipt.SHA256 != hash {
		t.Fatalf("fetch receipt: %+v", receipt)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != content {
		t.Fatalf("fetched %q: %v", got, err)
	}
	failure(t, fetch(out), "output-exists")

	appendArgs := []string{"artifact", "add", out, "--format=json", "--run", r.ID, "--type=application/json", "--sensitivity=sensitive"}
	appended := artifactData[app.ArtifactManifest](t, h.Run(t, appendArgs...).Success(t))
	if appended.ID == first.ID || !reflect.DeepEqual(appended.Run, first.Run) || appended.Payload.Location != p.Location {
		t.Fatalf("append changed the run or existing artifact identity: %+v", appended)
	}
	failure(t, h.Run(t, append(slices.Clone(appendArgs), "--summary=changed")...), "usage")
	writeArtifactFile(t, filepath.Join(h.Dir, name), "{}")
	second := add()
	if second.Run.ID == first.Run.ID || second.ID == first.ID || second.Payload.Location == p.Location {
		t.Fatalf("new run replaced existing identity: %+v", second)
	}
	if manifests := list("--project", project, "--run", r.ID); !reflect.DeepEqual(manifests, []app.ArtifactManifest{first, appended}) {
		t.Fatalf("wrong run contents: %+v", manifests)
	}
	if manifests := list(); len(manifests) != 3 {
		t.Fatalf("expected three artifacts: %+v", manifests)
	}

	other := filepath.Join(h.Dir, "other-project")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	h.Run(t, "project", "bind", other, "--slug=other", "--format=json").Success(t)
	if manifests := list("--project=other", "--run", r.ID); len(manifests) != 0 {
		t.Fatalf("run visible outside its Project: %+v", manifests)
	}
	failure(t, h.Run(t, "artifact", "inspect", first.ID, "--project=other", "--format=json"), "artifact-not-found")
	failure(t, h.Run(t, append(slices.Clone(appendArgs), "--project=other")...), "run-not-found")

	if err := os.WriteFile(cached, bytes.Repeat([]byte("x"), len(content)), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(h.Dir, "corrupt.json")
	failure(t, fetch(bad), "artifact-hash-mismatch")
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("corrupt payload published: %v", err)
	}
	failure(t, fetch(out), "artifact-hash-mismatch")
	if got, err := os.ReadFile(out); err != nil || string(got) != content {
		t.Fatalf("corruption changed existing output: %q %v", got, err)
	}
	if err := os.Remove(cached); err != nil {
		t.Fatal(err)
	}
	failure(t, fetch(bad), "artifact-unavailable")
}

func TestArtifactCacheLimitCountsUniqueContent(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	writeArtifactFile(t, filepath.Join(h.Dir, "one"), "one")
	writeArtifactFile(t, filepath.Join(h.Dir, "two"), "two")
	h.ExtraEnv = []string{"CTXFERRY_ARTIFACT_CACHE_BYTES=3"}
	h.Run(t, artifactAddArgs("one")...).Success(t)
	h.Run(t, artifactAddArgs("one")...).Success(t)
	failure(t, h.Run(t, artifactAddArgs("two")...), "artifact-cache-full")
	list := h.Run(t, "artifact", "list", "--format=json").Success(t)
	if got := artifactData[app.ArtifactList](t, list).Artifacts; len(got) != 2 {
		t.Fatalf("rejected payload left a manifest: %+v", got)
	}
	h.ExtraEnv = []string{"CTXFERRY_ARTIFACT_CACHE_BYTES=6"}
	h.Run(t, artifactAddArgs("two")...).Success(t)
	h.ExtraEnv = []string{"CTXFERRY_ARTIFACT_CACHE_BYTES=0"}
	failure(t, h.Run(t, artifactAddArgs("one")...), "usage")
}

func TestConcurrentArtifactAddsShareCacheLimit(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	writeArtifactFile(t, filepath.Join(h.Dir, "one"), "one")
	writeArtifactFile(t, filepath.Join(h.Dir, "two"), "two")
	h.ExtraEnv = []string{"CTXFERRY_ARTIFACT_CACHE_BYTES=3"}
	results := make(chan testcli.Result, 2)
	for _, name := range []string{"one", "two"} {
		go func() { results <- h.Run(t, artifactAddArgs(name)...) }()
	}
	adds := 0
	for range 2 {
		select {
		case r := <-results:
			if r.ExitCode == 0 {
				r.Success(t)
				adds++
			} else {
				failure(t, r, "artifact-cache-full")
			}
		case <-time.After(25 * time.Second):
			t.Fatal("artifact additions did not finish")
		}
	}
	if adds != 1 {
		t.Fatalf("expected one successful registration, got %d", adds)
	}
}
