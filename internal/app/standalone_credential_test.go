package app

import (
	"encoding/json"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

func TestCompatibleSecretSeparatesStandaloneCredentials(t *testing.T) {
	slot := CredentialRequirement{Name: "login", ResourceSlot: "db", Purpose: "runtime", Type: "token", Env: "DB"}
	standalone := CredentialRequirement{Name: "api", Purpose: "runtime", Type: "token", Env: "API_KEY"}
	attached := SecretBindingSpec{ResourceID: "resource", Purpose: "runtime", Type: "token"}
	free := SecretBindingSpec{Purpose: "runtime", Type: "token"}
	for _, c := range []struct {
		name     string
		req      CredentialRequirement
		resource string
		binding  SecretBindingSpec
		want     bool
	}{
		{"slot binding for its Resource", slot, "resource", attached, true},
		{"slot binding for another Resource", slot, "other", attached, false},
		{"slot without a mapped Resource", slot, "", free, false},
		{"standalone binding for a slot", slot, "resource", free, false},
		{"standalone binding", standalone, "", free, true},
		{"slot binding for a standalone requirement", standalone, "", attached, false},
		{"standalone binding of another type", standalone, "", SecretBindingSpec{Purpose: "runtime", Type: "password"}, false},
		{"standalone binding of another purpose", standalone, "", SecretBindingSpec{Purpose: "migration", Type: "token"}, false},
	} {
		if got := compatibleSecret(c.req, c.resource, c.binding); got != c.want {
			t.Errorf("%s: got %t", c.name, got)
		}
	}
}

func TestTaskValidationAllowsStandaloneAndExecOnly(t *testing.T) {
	base := TaskSpec{Name: "agent", Purpose: "Agent", MembershipID: "00000000-0000-4000-8000-000000000000",
		WorkingDirectory: ".", Program: "tools/run", Argv: []string{}, Inputs: []string{}, Platforms: []string{},
		CredentialRequirements: []CredentialRequirement{{Name: "api", Purpose: "model-api", Type: "api-key", Env: "OPENAI_API_KEY"}}}
	if err := validateTask(base); err != nil {
		t.Fatalf("standalone requirement refused: %v", err)
	}
	dangling := base
	dangling.CredentialRequirements = []CredentialRequirement{{Name: "api", ResourceSlot: "db", Purpose: "p", Type: "t", Env: "X"}}
	if validateTask(dangling) == nil {
		t.Fatal("a requirement naming a missing slot was accepted")
	}
	execOnly := base
	execOnly.Program, execOnly.AllowCustomCommand = "", true
	if err := validateTask(execOnly); err != nil {
		t.Fatalf("exec-only Task refused: %v", err)
	}
	for name, edit := range map[string]func(*TaskSpec){
		"no program without custom commands": func(s *TaskSpec) { s.AllowCustomCommand = false },
		"arguments without a program":        func(s *TaskSpec) { s.Argv = []string{"x"} },
	} {
		spec := execOnly
		edit(&spec)
		if validateTask(spec) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStandaloneBindingHasNoResourceDependency(t *testing.T) {
	standalone := remoteRecord{publishedEnvelope: publishedEnvelope{Kind: secretBindingKind, Scope: Scope{"project", "p"}}}
	if deps := dependencies(standalone); len(deps) != 1 || deps[0] != "p" {
		t.Fatalf("standalone binding dependencies: %v", deps)
	}
	if invalid := invalidTaskDependencies(standalone, map[string]remoteRecord{}); len(invalid) != 0 {
		t.Fatalf("standalone binding reported missing dependencies: %v", invalid)
	}
	attached := standalone
	attached.SecretBinding.ResourceID = "resource"
	if invalid := invalidTaskDependencies(attached, map[string]remoteRecord{}); len(invalid) != 1 {
		t.Fatalf("a missing Resource was not reported: %v", invalid)
	}
}

// Slot-attached credentials encode exactly as before standalone credentials
// existed, so devices keep their enabled digests.
func TestReviewedEncodingKeepsSlotAttachedDigests(t *testing.T) {
	type legacyCredential struct {
		Requirement     string           `json:"requirement"`
		Env             string           `json:"env"`
		Purpose         string           `json:"purpose"`
		Type            string           `json:"type"`
		ResourceSlot    string           `json:"resourceSlot"`
		ResourceID      string           `json:"resourceId"`
		ResourcePurpose string           `json:"resourcePurpose"`
		BindingID       string           `json:"bindingId"`
		ProviderProfile provider.Profile `json:"providerProfile"`
		Locator         provider.Locator `json:"locator"`
	}
	c := ReviewedCredential{Requirement: "login", Env: "DATABASE_URL", Purpose: "runtime", Type: "connection-string",
		ResourceSlot: "db", ResourceID: "resource", ResourcePurpose: "Integration", BindingID: "binding",
		ProviderProfile: provider.Profile{Provider: "fake", Name: "team", Method: "machine"},
		Locator:         provider.Locator{ProjectID: "p", Environment: "dev", Path: "/", Key: "DB"}}
	current, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(legacyCredential(c))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(legacy) {
		t.Fatalf("slot-attached encoding changed:\n%s\n%s", current, legacy)
	}
	c.ResourceSlot, c.ResourceID, c.ResourcePurpose = "", "", ""
	if standalone, _ := json.Marshal(c); json.Valid(standalone) && string(standalone) == string(legacy) {
		t.Fatal("standalone encoding matched the slot-attached one")
	}
}
