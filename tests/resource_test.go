package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func addResource(t *testing.T, h *testcli.Harness, identity, purpose, storage string, extra ...string) testcli.Result {
	t.Helper()
	args := []string{"resource", "add", "--identity", identity, "--kind", "database", "--stage", "dev",
		"--purpose", purpose, "--storage", storage, "--format=json"}
	return h.Run(t, append(args, extra...)...)
}

func resourceString(t *testing.T, r testcli.Response, key string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(r.Data[key], &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestResourceRoutesAndObservationsAcrossDevices(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	bind := func(h *testcli.Harness) {
		t.Helper()
		h.Run(t, "project", "bind", ".", "--project", "checkout", "--format=json").Success(t)
	}
	// Start with a v1 catalog containing only the pre-Resource record kinds.
	doc := obj(t, putDoc(t, a, "existing v1 notes", "older document", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	originalHead := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b := joinDevice(t, a, true)
	bind(b)
	b.Run(t, "context", "get", doc["id"].(string), "--format=json").Success(t)

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	for _, endpoint := range []string{address, "127.0.0.1.:5432", "[::ffff:127.0.0.1]:5432", "localhost:5432", "db.local:5432", "postgres://user:secret@db.example.com:5432"} {
		r := addResource(t, a, "rejected", "invalid endpoint", "shared", "--endpoint", endpoint)
		failure(t, r, "invalid-endpoint")
		if strings.Contains(r.Stdout+r.Stderr, "user:secret") {
			t.Fatal("rejected credentials echoed")
		}
	}
	local := addResource(t, a, "private-local-service", "local development", "local-only", "--endpoint", address).Success(t)
	registered := addResource(t, a, "test-db", "Run integration tests", "shared", "--endpoint", "db.example.invalid:5432",
		"--network", "dev-vpn", "--platform", runtime.GOOS+"/"+runtime.GOARCH).Success(t)
	configID := fieldID(t, setConfig(t, a, "project", "timeoutSeconds", "30"))
	id := resourceString(t, registered, "id")
	if resourceString(t, registered, "kind") != "database" || resourceString(t, registered, "stage") != "dev" ||
		string(registered.Data["endpoints"]) != `["db.example.invalid:5432"]` ||
		resourceString(t, registered, "network") != "dev-vpn" || string(registered.Data["platforms"]) != fmt.Sprintf(`[%q]`, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatal(registered)
	}
	inspect := func(h *testcli.Harness) testcli.Response {
		t.Helper()
		return h.Run(t, "resource", "inspect", id, "--format=json").Success(t)
	}
	setRoute := func(h *testcli.Harness, flag, target string) {
		t.Helper()
		res := h.Run(t, "resource", "route", id, flag, target, "--network", "dev-vpn", "--format=json").Success(t)
		if route := obj(t, res, "route"); route["source"] != "device-local" || route["status"] != "resolved" {
			t.Fatal(route)
		}
	}
	list := a.Run(t, "resource", "list", "--format=json").Success(t)
	if !strings.Contains(string(list.Data["resources"]), "Run integration tests") {
		t.Fatal(list)
	}
	if route := obj(t, inspect(a), "route"); route["status"] != "network-mismatch" {
		t.Fatal(route)
	}
	failure(t, a.Run(t, "resource", "check", id, "--format=json"), "network-mismatch")
	setRoute(a, "--endpoint", address)
	before := inspect(a)
	if string(before.Data["observations"]) != "[]" {
		t.Fatal(before)
	}
	show := a.Run(t, "context", "show", "--format=json").Success(t)
	if purps := string(show.Data["resources"]); !strings.Contains(purps, "Run integration tests") || strings.Contains(purps, address) {
		t.Fatal(show)
	}
	// The listener starts open before the read commands. No connection should
	// be queued until an explicit check.
	if err := listener.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err == nil {
		conn.Close()
		t.Fatal("a read or route change connected to the service")
	}
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
	check := func(h *testcli.Harness, status, scopeKind string) testcli.Response {
		t.Helper()
		r := h.Run(t, "resource", "check", id, "--format=json").Success(t)
		scope := obj(t, r, "scope")
		if resourceString(t, r, "status") != status || resourceString(t, r, "tool") != "tcp-connect" || string(r.Data["serviceVerified"]) != "false" ||
			resourceString(t, r, "resourceId") != id || scope["kind"] != scopeKind || scope["target"] != address ||
			scope["network"] != "dev-vpn" || scope["os"] != runtime.GOOS || scope["arch"] != runtime.GOARCH ||
			scope["resourceRevision"] != resourceString(t, registered, "revision") {
			t.Fatal(r)
		}
		if _, err := time.Parse(time.RFC3339Nano, resourceString(t, r, "observedAt")); err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := check(a, "reachable", "service-endpoint")
	a.Run(t, "sync", "--format=json").Success(t)
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	path := "records/" + id[:2] + "/" + id + ".json"
	configPath := "records/" + configID[:2] + "/" + configID + ".json"
	diff := strings.Fields(git(t, a, "--git-dir", remote, "diff", "--name-only", originalHead, head))
	if len(diff) != 2 || !slices.Contains(diff, path) || !slices.Contains(diff, configPath) {
		t.Fatal("unexpected published changes", diff)
	}
	raw := git(t, a, "--git-dir", remote, "show", head+":"+path)
	for _, sentinel := range []string{address, "private-local-service", resourceString(t, local, "id"), resourceString(t, first, "id"), "tcp-connect", "observations", "proxy"} {
		if strings.Contains(raw, sentinel) {
			t.Fatal("device-local data published", sentinel)
		}
	}
	b.Run(t, "sync", "--format=json").Success(t)
	effectiveField(t, explain(t, b), "timeoutSeconds", "project", float64(30))
	failure(t, b.Run(t, "resource", "inspect", resourceString(t, local, "id"), "--format=json"), "resource-not-found")
	onB := inspect(b)
	if obj(t, onB, "route")["status"] != "network-mismatch" || string(onB.Data["observations"]) != "[]" ||
		!reflect.DeepEqual(obj(t, before, "resource"), obj(t, onB, "resource")) {
		t.Fatal(onB)
	}
	c := joinDevice(t, a, true)
	bind(c)
	effectiveField(t, explain(t, c), "timeoutSeconds", "project", float64(30))
	if detail := inspect(c); !reflect.DeepEqual(obj(t, onB, "resource"), obj(t, detail, "resource")) ||
		obj(t, detail, "route")["status"] != "network-mismatch" || string(detail.Data["observations"]) != "[]" {
		t.Fatal(detail)
	}
	setRoute(b, "--proxy", address)
	second := check(b, "reachable", "proxy-endpoint")
	if resourceString(t, first, "deviceId") == resourceString(t, second, "deviceId") {
		t.Fatal("simulated Devices share an identity")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	third := check(a, "unreachable", "service-endpoint")
	for _, h := range []*testcli.Harness{b, a} {
		h.Run(t, "sync", "--format=json").Success(t)
	}
	if got := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); got != head {
		t.Fatal("local checks or route facts published a commit", got)
	}
	for _, tc := range []struct {
		h    *testcli.Harness
		want []testcli.Response
	}{{a, []testcli.Response{third, first}}, {b, []testcli.Response{second}}} {
		detail := inspect(tc.h)
		if !reflect.DeepEqual(obj(t, before, "resource"), obj(t, detail, "resource")) {
			t.Fatal("check changed Resource intent", detail)
		}
		var history []map[string]json.RawMessage
		if err := json.Unmarshal(detail.Data["observations"], &history); err != nil || len(history) != len(tc.want) {
			t.Fatalf("wrong history: %s (%v)", detail.Data["observations"], err)
		}
		for i, want := range tc.want {
			if !reflect.DeepEqual(history[i], want.Data) {
				t.Fatal("recorded observation differs from check result", history[i], want)
			}
		}
		if len(history) == 2 && string(history[0]["id"]) == string(history[1]["id"]) {
			t.Fatal("checks reused an observation identity")
		}
	}

	// A shared Resource must reference a live Project in the same v1 snapshot.
	catalog := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, catalog)
	var record map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	record["scope"] = json.RawMessage(`{"kind":"project","id":"00000000-0000-4000-8000-000000000001"}`)
	orphan, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalog, filepath.FromSlash(path)), orphan, 0600); err != nil {
		t.Fatal(err)
	}
	git(t, a, "-C", catalog, "add", "--", path)
	git(t, a, "-C", catalog, "commit", "-m", "Orphan Resource")
	git(t, a, "-C", catalog, "push", "origin", "ctxferry-v1")
	failure(t, b.Run(t, "sync", "--format=json"), "incomplete")
	if !reflect.DeepEqual(obj(t, onB, "resource"), obj(t, inspect(b), "resource")) {
		t.Fatal("invalid snapshot changed the installed Resource")
	}
}

func TestResourceRouteConditions(t *testing.T) {
	h := contextHarness(t)
	add := func(extra ...string) string {
		t.Helper()
		return resourceString(t, addResource(t, h, "service", "Check route conditions", "local-only", extra...).Success(t), "id")
	}
	for _, tc := range []struct {
		want string
		args []string
	}{
		{"platform-unsupported", []string{"--platform", "unknown/unknown"}},
		{"route-missing", nil},
		{"route-ambiguous", []string{"--endpoint", "localhost:1", "--endpoint", "localhost:2"}},
	} {
		id := add(tc.args...)
		inspected := h.Run(t, "resource", "inspect", id, "--format=json").Success(t)
		if obj(t, inspected, "route")["status"] != tc.want || string(inspected.Data["observations"]) != "[]" {
			t.Fatal(inspected)
		}
		failure(t, h.Run(t, "resource", "check", id, "--format=json"), tc.want)
	}
}

func TestContextShowPaginatesDocumentsResourcesAndTasksTogether(t *testing.T) {
	h := gitHarness(t)
	binding := obj(t, bind(t, h), "binding")
	membership := binding["membershipId"].(string)
	h.Run(t, "profile", "create", "development", "--format=json").Success(t)
	h.Run(t, "profile", "select", "development", "--format=json").Success(t)
	setConfig(t, h, "profile", "parallelism", "3", "--profile=development")

	// Recreate the schema from before Resource support, retaining existing
	// shared configuration and this checkout's local Profile selection.
	db, err := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`DROP TABLE resource_observations; DROP TABLE resource_routes; PRAGMA user_version=7;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	effectiveField(t, explain(t, h), "parallelism", "profile", float64(3))

	var wantIDs []string
	for i := range 12 {
		title := fmt.Sprintf("%02d%s", i, strings.Repeat("<", 158))
		purpose := strings.Repeat("&", 160)
		r := addResource(t, h, title, purpose, "local-only").Success(t)
		d := obj(t, putDoc(t, h, title, "body", "local-only", "full", "--purpose", purpose).Success(t), "document")
		task := addTask(t, h, fmt.Sprintf("task-%d", i), membership, "unused", "local-only", "--purpose", purpose).Success(t)
		wantIDs = append(wantIDs, resourceString(t, r, "id"), d["id"].(string), resourceString(t, task, "id"))
	}
	slices.Sort(wantIDs)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var got []string
			cursor := ""
			for page := 0; ; page++ {
				r := h.Run(t, "context", "show", "--cursor", cursor, "--format", format)
				if r.ExitCode != 0 || r.Stderr != "" || len(r.Stdout) > 8192 {
					t.Fatalf("page %d, exit %d, size %d, stderr %s", page, r.ExitCode, len(r.Stdout), r.Stderr)
				}
				var data struct {
					Documents, Resources, Tasks []struct{ ID string }
					Profile                     struct{ Name string }
					Truncated                   bool
					NextCursor                  string
				}
				raw := []byte(r.Stdout)
				if format == "json" {
					r.Success(t)
					var env struct{ Data json.RawMessage }
					if err := json.Unmarshal(raw, &env); err != nil {
						t.Fatal(err)
					}
					raw = env.Data
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					t.Fatal(err)
				}
				if data.Profile.Name != "development" {
					t.Fatal("selected Profile missing after schema 7 upgrade", data.Profile)
				}
				var pageIDs []string
				for _, d := range data.Documents {
					pageIDs = append(pageIDs, d.ID)
				}
				for _, resource := range data.Resources {
					pageIDs = append(pageIDs, resource.ID)
				}
				for _, task := range data.Tasks {
					pageIDs = append(pageIDs, task.ID)
				}
				slices.Sort(pageIDs)
				if len(pageIDs) == 0 || pageIDs[0] <= cursor {
					t.Fatal("empty or repeated page", pageIDs, cursor)
				}
				got = append(got, pageIDs...)
				if !data.Truncated {
					if page == 0 || data.NextCursor != "" {
						t.Fatal("expected several pages and no final cursor", data)
					}
					break
				}
				if data.NextCursor != pageIDs[len(pageIDs)-1] || len(got) >= len(wantIDs) {
					t.Fatal("invalid continuation", pageIDs, data.NextCursor)
				}
				cursor = data.NextCursor
			}
			if !slices.Equal(got, wantIDs) {
				t.Fatal("pagination missed or repeated entries", got, wantIDs)
			}
		})
	}
}
