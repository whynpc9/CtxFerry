package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/store"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func doctor(t *testing.T, h *testcli.Harness, args ...string) app.DoctorReport {
	t.Helper()
	result := h.Run(t, append([]string{"doctor", "--format=json"}, args...)...)
	result.Success(t) // Includes exactly one JSON object and no stderr.
	if strings.Contains(result.Stdout, "doctor-sentinel") || strings.Contains(result.Stdout, "credential-sentinel") {
		t.Fatal("doctor disclosed a credential, backend locator, or unrelated environment value")
	}
	var wire struct{ Data app.DoctorReport }
	if err := json.Unmarshal([]byte(result.Stdout), &wire); err != nil {
		t.Fatal(err)
	}
	return wire.Data
}

func TestDoctorHealthyAndStaleSync(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, _ := syncHarness(t, &metadata)
	git(t, h, "init")
	git(t, h, "remote", "add", "origin", "https://doctor-sentinel:doctor-sentinel@github.com/example/checkout.git")
	enableSync(t, h)
	before := doctor(t, h)
	if before.LastSync.Status != "unknown" || before.LastSync.LastSyncAt != nil {
		t.Fatalf("configured but never synced: %+v", before.LastSync)
	}

	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	fresh := doctor(t, h)
	if fresh.CLI.Status != "available" || fresh.Catalog.Status != "available" ||
		fresh.DirectoryBinding.Status != "available" || fresh.DirectoryBinding.Source != "path-binding" ||
		fresh.PathResolution.Status != "available" || fresh.PathResolution.GitCheckout == "" ||
		fresh.PathResolution.WorkingDirectory.Status != "available" || fresh.Dependencies[0].Status != "available" ||
		fresh.LastSync.Status != "available" || fresh.LastSync.LastSyncAt == nil || fresh.LastSync.State != "synced" ||
		fresh.GitAuth.Status != "unknown" || fresh.GitAuth.LastSyncState != "synced" {
		t.Fatalf("healthy local and historical checks: %+v", fresh)
	}

	db, err := sql.Open("sqlite", store.Path(h.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	if _, err := db.Exec("UPDATE sync_state SET last_sync_at=?,outcome='auth-required'", old.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	stale := doctor(t, h)
	if stale.LastSync.Status != "stale" || stale.LastSync.LastSyncAt == nil || !old.Equal(*stale.LastSync.LastSyncAt) ||
		stale.LastSync.State != "auth-required" || stale.GitAuth.Status != "unknown" ||
		stale.GitAuth.LastSyncState != "auth-required" || stale.DirectoryBinding.Status != "available" {
		t.Fatalf("stale sync should coexist with locally available capabilities: %+v", stale)
	}

	setConfig(t, h, "project", "workingDirectory", `"missing"`)
	paths := doctor(t, h).PathResolution
	if paths.Status != "available" || paths.WorkingDirectory.Status != "unavailable" {
		t.Fatalf("current and configured path checks must remain independent: %+v", paths)
	}
}

func TestDoctorUnboundAndUninitialized(t *testing.T) {
	h := testcli.New(t, binary)
	fresh := doctor(t, h)
	if fresh.CLI.Status != "available" || fresh.Catalog.Status != "unavailable" || fresh.DirectoryBinding.Status != "unavailable" ||
		fresh.PathResolution.Status != "available" || fresh.LastSync.Status != "unknown" {
		t.Fatalf("uninitialized host: %+v", fresh)
	}
	if _, err := os.Stat(h.DataDir); !os.IsNotExist(err) {
		t.Fatal("doctor initialized a data directory")
	}
	h.Run(t, "init", "--format=json").Success(t)
	h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	// An explicitly selected Project must not count as a directory binding.
	h.Dir = t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h.GitPath = filepath.Dir(gitPath)
	r := doctor(t, h, "--project=checkout")
	if r.DirectoryBinding.Status != "unavailable" || r.PathResolution.Status != "available" ||
		r.PathResolution.WorkingDirectory.Status != "unavailable" || r.LastSync.State != "disabled" {
		t.Fatalf("unbound directory with explicit Project: %+v", r)
	}
}

func TestDoctorMissingProviderLoginAndHeadlessInjection(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, _ := syncHarness(t, &metadata)
	identity := h.Run(t, "init", "--format=json").Success(t)
	git(t, h, "init")
	mid := obj(t, bind(t, h, "--project=checkout"), "binding")["membershipId"].(string)
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	task := addTask(t, h, "check", mid, "unused", "shared", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential).Success(t)
	taskID := resourceString(t, task, "id")
	resource := addResource(t, h, "db", "Integration service", "shared").Success(t)
	resourceID := resourceString(t, resource, "id")
	h.Run(t, "profile", "map-resource", "dev", taskID, "db", resourceID, "--format=json").Success(t)
	h.Run(t, "secret", "bind", taskID, "login", "--profile=dev", "--alias=db",
		"--provider=infisical", "--provider-profile=team", "--method=universal-auth", "--entry-id=doctor-sentinel-locator", "--format=json").Success(t)

	// Explicit nonexistent Unix bus in the harness, including on hosts with a desktop session.
	h.ExtraEnv = []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(h.Home, "absent-bus"),
		"UNRELATED_ENV=doctor-sentinel-unrelated", "INFISICAL_CLIENT_SECRET=doctor-sentinel-client",
		"GH_TOKEN=doctor-sentinel-github", "SSH_AUTH_SOCK=doctor-sentinel-agent"}
	first := doctor(t, h)
	if len(first.ProviderAuth.Profiles) != 1 || first.ProviderAuth.Profiles[0].Headless != "not-configured" ||
		first.ProviderAuth.Profiles[0].Profile.Name != "team" || len(first.GitAuth.Mechanisms) != 2 {
		t.Fatalf("missing provider login: %+v", first)
	}
	if runtime.GOOS == "linux" && (first.CredentialStore.Status != "unavailable" || first.ProviderAuth.Profiles[0].Status != "unavailable") {
		t.Fatalf("headless host should report inaccessible credential storage and no usable provider login: %+v", first)
	}

	h.ExtraEnv = append(h.ExtraEnv, "CTXFERRY_INFISICAL_ACCESS_TOKEN=doctor-sentinel-token",
		"CTXFERRY_INFISICAL_WORKSPACE_ID="+field(t, identity, "workspace"), "CTXFERRY_INFISICAL_PROFILE=team",
		"CTXFERRY_INFISICAL_DEVICE_ID="+field(t, identity, "device"))
	p := doctor(t, h).ProviderAuth.Profiles[0]
	if p.Headless != "scoped-runtime-session-configured" || p.Status != "unknown" {
		t.Fatalf("injected session metadata cannot verify live authentication: %+v", p)
	}
	h.ExtraEnv[len(h.ExtraEnv)-1] = "CTXFERRY_INFISICAL_DEVICE_ID=another-device"
	if p := doctor(t, h).ProviderAuth.Profiles[0]; p.Headless != "not-configured" {
		t.Fatalf("injection scope crossed devices: %+v", p)
	}
}

func TestDoctorResourceIdentityAndReadOnlyCatalog(t *testing.T) {
	h := contextHarness(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	res := addResource(t, h, "db", "Integration service", "local-only",
		"--endpoint="+listener.Addr().String()).Success(t)
	id := resourceString(t, res, "id")
	facts := doctor(t, h).Resources.Items
	if len(facts) != 1 || facts[0].TCPReachability != "unknown" || facts[0].ServiceIdentity != "unknown" {
		t.Fatalf("doctor must not probe a listening port: %+v", facts)
	}
	h.Run(t, "resource", "check", id, "--format=json").Success(t)
	path := store.Path(h.DataDir)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	facts = doctor(t, h).Resources.Items
	if len(facts) != 1 || facts[0].TCPReachability != "reachable" || facts[0].TCPObservedAt == nil ||
		facts[0].ServiceIdentity != "unknown" || facts[0].Route != "resolved" {
		t.Fatalf("a stored TCP observation must not verify service identity: %+v", facts)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("doctor modified the catalog")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := doctor(t, h).Catalog; got.Status != "unavailable" || !strings.Contains(got.Evidence, "migration") {
		t.Fatalf("doctor migrated the catalog or failed to identify its schema: %+v", got)
	}
}

func TestDoctorBackgroundSyncServiceStates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses an isolated systemd user-manager fixture")
	}
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	check := func(status string) {
		t.Helper()
		if got := doctor(t, h).BackgroundSync; got.Status != status {
			t.Fatalf("background sync: got %+v, want %s", got, status)
		}
	}
	check("unavailable")

	manager := t.TempDir()
	tool := filepath.Join(manager, "systemctl")
	script := `#!/bin/sh
[ "$1" = "--user" ] || exit 2
case "$2" in
  show|daemon-reload) exit 0 ;;
  enable) : > "$CTXFERRY_TEST_MANAGER/enabled" ;;
  restart) : > "$CTXFERRY_TEST_MANAGER/running" ;;
  is-enabled) test -f "$CTXFERRY_TEST_MANAGER/enabled" ;;
  is-active) test -f "$CTXFERRY_TEST_MANAGER/running" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h.GitPath = manager
	h.ExtraEnv = []string{"CTXFERRY_TEST_MANAGER=" + manager, "XDG_CONFIG_HOME=" + h.Home}
	check("foreground-only")
	if files, err := os.ReadDir(h.Home); err != nil || len(files) != 0 {
		t.Fatalf("doctor wrote service configuration: %v %v", files, err)
	}

	installed := h.Run(t, "sync", "install-service", "--format=json").Success(t)
	if string(installed.Data["manager"]) != `"systemd"` ||
		string(installed.Data["installed"]) != "true" || string(installed.Data["running"]) != "true" {
		t.Fatal(installed)
	}
	check("running")
	if err := os.Remove(filepath.Join(manager, "running")); err != nil {
		t.Fatal(err)
	}
	check("installed")
}
