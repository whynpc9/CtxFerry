package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/bitwarden"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func TestBitwardenScopedSession(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "Source checkout")
	bwDir := t.TempDir()
	name := "bw"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bwDir, name), "./testdata/bw").CombinedOutput(); err != nil {
		t.Fatalf("build bw fixture: %v\n%s", err, out)
	}
	fixture := taskFixture(t)
	installTaskFixture(t, h.Dir, fixture, "isolated Bitwarden device")
	h.Dir = filepath.Join(h.Dir, "app")

	init := h.Run(t, "init", "--format=json").Success(t)
	workspace, device := field(t, init, "workspace"), field(t, init, "device")
	h.ExtraEnv = append(h.ExtraEnv,
		"PATH="+strings.Join([]string{bwDir, filepath.Dir(binary), h.GitPath}, string(os.PathListSeparator)),
		"BITWARDENCLI_APPDATA_DIR="+t.TempDir(), "BW_PASSWORD="+providertest.SyntheticSession+"-master",
		"BW_CLEANEXIT=1", "CTXFERRY_BITWARDEN_PROFILE=team", "BW_SESSION="+providertest.SyntheticSession,
	)
	base := append([]string(nil), h.ExtraEnv...)
	mid := obj(t, bind(t, h, "--project=checkout"), "binding")["membershipId"].(string)
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	addTask(t, h, "check", mid, "tools/"+filepath.Base(fixture), "shared", "--working-directory=working",
		"--resource-slot="+dbSlot, "--credential-requirement="+dbCredential, "--arg=credentials", "--arg=DATABASE_URL").Success(t)
	resourceID := resourceString(t, addResource(t, h, "db", "Integration service", "shared").Success(t), "id")
	h.Run(t, "profile", "map-resource", "dev", "check", "db", resourceID, "--format=json").Success(t)
	bindItem := func(alias, id string) testcli.Result {
		return h.Run(t, "secret", "bind", "check", "login", "--profile=dev", "--alias", alias, "--entry-id", id,
			"--provider="+bitwarden.Name, "--method="+bitwarden.Method, "--provider-profile=team", "--format=json")
	}
	failure(t, bindItem("fuzzy", "similar item"), "invalid-secret-binding")
	const itemID = "32f7cf3b-cb32-4944-8eeb-17d088b5a588"
	binding := field(t, bindItem("primary-db", itemID).Success(t), "binding")

	status := func(wantCode string, unlocked bool, validity string) map[string]any {
		t.Helper()
		r := h.Run(t, "secret", "status", "check", "login", "--profile=dev", "--format=json")
		assertNoSecretValues(t, r.Stdout+r.Stderr)
		if strings.Contains(r.Stdout+r.Stderr, "fixture@example.invalid") {
			t.Fatal("raw bw metadata escaped into status")
		}
		facts := secretFacts(t, r, wantCode)
		evidence, ok := facts["authentication"].(map[string]any)
		if !ok || evidence["loggedIn"] != true || evidence["unlocked"] != unlocked || evidence["sessionValidity"] != validity ||
			evidence["expiresAt"] != nil || facts["authenticated"] != unlocked {
			t.Fatal("unexpected Bitwarden login, unlock or session evidence")
		}
		return facts
	}

	// An ambient BW_SESSION or a launcher scoped to a different device does
	// not unlock this binding, even though bw has a logged-in local account.
	status("authentication-required", false, "absent")
	h.ExtraEnv = append(append([]string(nil), base...), "CTXFERRY_BITWARDEN_WORKSPACE_ID="+workspace, "CTXFERRY_BITWARDEN_DEVICE_ID=another-device")
	status("authentication-required", false, "absent")

	h.ExtraEnv = append(append([]string(nil), base...), "CTXFERRY_BITWARDEN_WORKSPACE_ID="+workspace, "CTXFERRY_BITWARDEN_DEVICE_ID="+device,
		"BW_SESSION=stale-"+providertest.SyntheticSession)
	status("authentication-required", false, "invalid")
	h.ExtraEnv = h.ExtraEnv[:len(h.ExtraEnv)-1]
	if facts := status("", true, "valid"); facts["entryRetrievable"] != true {
		t.Fatal("the exact cached item was not available")
	}

	// Exercise explicit adoption with a synthetic OS credential store.
	t.Setenv("PATH", bwDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BITWARDENCLI_APPDATA_DIR", t.TempDir())
	t.Setenv("CTXFERRY_BITWARDEN_PROFILE", "team")
	t.Setenv("BW_SESSION", providertest.SyntheticSession)
	credentials := &providertest.MemoryStore{}
	providers := provider.Services{
		Credentials: credentials, Adapters: map[string]provider.Adapter{bitwarden.Name: bitwarden.New()},
	}
	profile := provider.Profile{Provider: bitwarden.Name, Name: "team", Method: bitwarden.Method}
	providerCommand(t, h, providers, "provider", "login", profile.Provider, "--provider-profile", profile.Name, "--method", profile.Method).Success(t)
	t.Setenv("BW_SESSION", "")
	adopted := providerCommand(t, h, providers, "secret", "status", "check", "login", "--profile=dev")
	if facts := secretFacts(t, adopted, ""); facts["entryRetrievable"] != true {
		t.Fatal("the adopted key was not usable from the device credential store")
	}

	preview := h.Run(t, "task", "trust", "preview", "check", "--profile=dev", "--format=json").Success(t)
	var token string
	if err := json.Unmarshal(preview.Data["preview"], &token); err != nil || token == "" {
		t.Fatal("execution trust preview did not return a token")
	}
	h.Run(t, "task", "trust", "enable", "check", "--profile=dev", "--preview", token, "--format=json").Success(t)
	run := h.Run(t, "run", "check", "--profile=dev", "--format=jsonl")
	assertNoSecretValues(t, run.Stdout+run.Stderr)
	assertChildEnvironment(t, reportedEnvironment(t, run), "DATABASE_URL", secretValue)

	bindItem("missing-db", "32f7cf3b-cb32-4944-8eeb-17d088b5a589").Success(t)
	if facts := status("secret-unavailable", true, "valid"); facts["entryRetrievable"] != false {
		t.Fatal("a missing cached item must be unavailable while session remains valid")
	}

	enableSync(t, h)
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	published := git(t, h, "--git-dir", remote, "show", "ctxferry-v1:records/"+binding[:2]+"/"+binding+".json")
	if !strings.Contains(published, itemID) {
		t.Fatal("published binding lost its item reference")
	}
	assertNoSecretValues(t, published)
	assertNoPersistedValues(t, h.DataDir)
}
