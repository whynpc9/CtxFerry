package tests

import (
	"encoding/json"
	"net"
	"slices"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
)

func TestDesktopCredentialAndResourceActions(t *testing.T) {
	h := gitHarness(t)
	ctx := t.Context()
	bound := bind(t, h, "--slug=checkout")
	project := obj(t, bound, "project")["id"].(string)
	membership := obj(t, bound, "binding")["membershipId"].(string)
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	task := resourceString(t, addTask(t, h, "check", membership, "unused", "shared",
		"--resource-slot="+dbSlot, "--credential-requirement="+dbCredential).Success(t), "id")
	resource := resourceString(t, addResource(t, h, "test-db", "Integration service", "shared").Success(t), "id")
	h.Run(t, "profile", "map-resource", "dev", task, "db", resource, "--format=json").Success(t)

	profile := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	ref := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "BACKEND_DB"}
	fake := &providertest.Fake{
		Methods: []provider.MethodCapabilities{{Method: profile.Method}},
		Entries: map[providertest.Reference]string{{Profile: profile, Locator: ref}: secretValue},
	}
	core := app.App{DataDir: h.DataDir, Providers: provider.Services{
		Adapters: map[string]provider.Adapter{profile.Provider: fake}, Credentials: &providertest.MemoryStore{},
	}}
	target := app.SecretTarget{Profile: "dev", Task: task, Requirement: "login"}
	read := func() app.DesktopCredentialSlot {
		t.Helper()
		slot, err := core.DesktopSecretSlot(ctx, project, target)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(slot)
		if err != nil {
			t.Fatal(err)
		}
		assertNoProviderData(t, string(wire))
		return slot
	}

	catalog, err := core.DesktopCredentials(ctx, project)
	if err != nil || !slices.Equal(catalog.Profiles, []string{"dev"}) || len(catalog.Tasks) != 1 ||
		catalog.Tasks[0].ID != task || len(catalog.Tasks[0].Slots) != 1 {
		t.Fatalf("desktop selection metadata: %+v, %v", catalog, err)
	}
	before := read()
	if before.Issue != "binding-missing" || before.Resource.ID != resource || before.Status.Bound {
		t.Fatal("missing mapping not isolated from resource metadata")
	}
	change := app.SecretBindingChange{Alias: "integration", ProviderProfile: profile, Locator: ref}
	receipt, err := core.BindDesktopSecret(ctx, before, change)
	if err != nil || receipt.Alias != change.Alias || receipt.State != "local" {
		t.Fatalf("desktop save: %+v, %v", receipt, err)
	}
	if _, err := core.BindDesktopSecret(ctx, before, change); err == nil || app.Classify(err).Code != "selection-changed" {
		t.Fatalf("obsolete mapping confirmation accepted: %v", err)
	}
	after := read()
	if !after.Status.Bound || after.Status.Authenticated != nil || after.Status.EntryRetrievable != nil ||
		len(after.Reusable) != 1 || after.Reusable[0].ID != after.Status.Binding.ID || fake.LoginCount() != 0 || len(fake.Lookups()) != 0 {
		t.Fatal("reading binding metadata supplied inferred status or used the provider")
	}
	check := func(entry bool, code string) app.SecretStatus {
		t.Helper()
		status, err := core.CheckDesktopSecret(ctx, after, entry)
		if (code == "" && err != nil) || (code != "" && (err == nil || app.Classify(err).Code != code)) {
			t.Fatalf("check returned unexpected status code: %v", err)
		}
		wire, err := json.Marshal(status)
		if err != nil {
			t.Fatal(err)
		}
		assertNoProviderData(t, string(wire))
		return status
	}
	signedOut := check(false, "authentication-required")
	if signedOut.Authenticated == nil || *signedOut.Authenticated || signedOut.EntryRetrievable != nil {
		t.Fatal("signed-out state conflated authentication and entry readability")
	}
	if _, err := core.LoginDesktopProvider(ctx, after); err != nil {
		t.Fatal(err)
	}
	authed := check(false, "")
	if authed.Authenticated == nil || !*authed.Authenticated || authed.EntryRetrievable != nil || len(fake.Lookups()) != 0 {
		t.Fatal("authentication alone read an entry or inferred availability")
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	h.Run(t, "resource", "route", resource, "--proxy", listener.Addr().String(), "--endpoint", "localhost:5432", "--format=json").Success(t)
	fleet, err := core.DesktopFleetView(ctx, project, resource)
	if err != nil || fleet.Probe == nil || fleet.Probe.Scope.Kind != "proxy-endpoint" ||
		fleet.Probe.Scope.Target != listener.Addr().String() || len(fleet.Devices) != 1 || len(fleet.Devices[0].Observations) != 0 {
		t.Fatalf("passive fleet route and observation metadata: %+v, %v", fleet, err)
	}
	h.Run(t, "resource", "route", resource, "--endpoint", listener.Addr().String(), "--format=json").Success(t)
	if _, err := core.ProbeDesktopResource(ctx, *fleet.Probe); err == nil || app.Classify(err).Code != "selection-changed" {
		t.Fatalf("obsolete probe target was accepted: %v", err)
	}
	fleet, err = core.DesktopFleetView(ctx, project, resource)
	if err != nil || fleet.Probe == nil || len(fleet.Devices[0].Observations) != 0 {
		t.Fatalf("reading the route performed or recorded a probe: %+v, %v", fleet, err)
	}
	observation, err := core.ProbeDesktopResource(ctx, *fleet.Probe)
	if err != nil || observation.Status != "reachable" || observation.DeviceID != after.Status.DeviceID || observation.ServiceVerified {
		t.Fatalf("local TCP probe: %+v, %v", observation, err)
	}
	fleet, err = core.DesktopFleetView(ctx, project, resource)
	if err != nil || len(fleet.Devices[0].Observations) != 1 || fleet.Devices[0].Observations[0].ID != observation.ID {
		t.Fatal("desktop did not reread its persisted observation", err)
	}
	available := check(true, "")
	if available.EntryRetrievable == nil || !*available.EntryRetrievable || available.ResourceReachable == nil ||
		!*available.ResourceReachable || !slices.Equal(fake.Lookups(), []providertest.Reference{{Profile: profile, Locator: ref}}) {
		t.Fatal("entry check did not use the exact binding or maintain separate reachability evidence")
	}
}
