package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

func TestNavigateCredentialsAndFleet(t *testing.T) {
	p := app.Project{ID: "project", Slug: "integration"}
	local := app.Device{ID: "local", Name: "desktop-test", OS: "darwin", Arch: "arm64"}
	resource := app.Resource{ID: "db", ResourceSpec: app.ResourceSpec{Identity: "test-database", Stage: "test", Purpose: "Integration checks"}}
	binding := app.SecretBinding{ID: "binding", SecretBindingSpec: app.SecretBindingSpec{
		Alias: "db-reader", ProviderProfile: provider.Profile{Provider: "infisical", Name: "test", Method: "personal"},
		Locator: provider.Locator{ProjectID: "backend", Environment: "dev", Path: "/db", Key: "DB_READER"},
	}}
	req := app.DesktopCredentialRequirement{Name: "login", Purpose: "runtime", Type: "connection-string"}
	d := desktop{route: desktopRoute{Page: pageOverview}, data: app.DesktopSnapshot{Device: local, ReadAt: time.Now()},
		projects:       desktopProjects{ready: true, items: []app.Project{p}},
		workProjects:   workProjects{loaded: true, items: []app.Project{p}},
		catalogProject: p.ID,
		secrets: secretsView{ready: true, project: p.ID, profile: "dev", task: "task", requirement: req.Name,
			catalog: app.DesktopCredentialCatalog{Profiles: []string{"dev"}, Tasks: []app.DesktopCredentialTask{
				{ID: "task", Name: "check-db", Slots: []app.DesktopCredentialRequirement{req}},
			}},
			slot: app.DesktopCredentialSlot{
				Status: app.SecretStatus{Project: p, TaskID: "task", Profile: "dev", Requirement: req.Name,
					Binding: &binding, Bound: true, Authenticated: new(false), ResourceReachable: new(true)},
				Resource: resource, Reusable: []app.SecretBinding{binding},
			}},
		fleet: fleetView{ready: true, project: p.ID, resource: resource.ID, data: app.DesktopFleet{
			Resources: []app.Resource{resource}, Selected: resource,
			Devices: []app.DesktopEnvironment{
				{Device: local, Local: true},
				{Device: app.Device{ID: "win", OS: "windows", Arch: "amd64"}},
				{Device: app.Device{ID: "wsl", OS: "linux", Arch: "amd64"}, Kind: "WSL"},
			},
			Probe: &app.DesktopResourceProbe{DeviceID: local.ID, Scope: app.ObservationScope{Target: "127.0.0.1:5432"}},
		}},
	}
	window := ui.NewTester(d.view, 1280, 1400)
	if err := window.Click("Catalog"); err != nil {
		t.Fatal(err)
	}
	if err := window.Click("Open Credential bindings"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Shared credential binding", "Mapped", "Sign-in required", "Entry readable", "TCP reachable",
		"No run evidence", "/db · DB_READER", "Change shared binding…", "Open Infisical console ↗"} {
		if _, ok := window.Access(label); !ok {
			t.Errorf("credential page missing %q", label)
		}
	}
	if _, ok := window.Access("Check entry readability"); ok {
		t.Error("entry check appeared before confirming authentication")
	}
	if err := window.Click("Open Resource test-database"); err != nil {
		t.Fatal(err)
	}
	if expected := (desktopRoute{Page: pageFleet, ProjectID: p.ID, RecordID: resource.ID}); d.route != expected {
		t.Fatalf("resource navigation lost its scope: %+v", d.route)
	}
	for _, label := range []string{"Windows · amd64", "WSL · wsl", "127.0.0.1:5432", "Probe TCP on this device…"} {
		if _, ok := window.Access(label); !ok {
			t.Errorf("fleet page missing %q", label)
		}
	}
	d.fleet.devices.Choose(2)
	window.Frame()
	if _, ok := window.Access("Session state is unknown on this device"); !ok {
		t.Error("remote selection did not report unknown provider state")
	}
	if _, ok := window.Access("Probe TCP on this device…"); ok {
		t.Error("probe was offered from a remote selection")
	}
	if err := window.Click("Home"); err != nil {
		t.Fatal(err)
	}
	if err := window.Click("Catalog"); err != nil {
		t.Fatal(err)
	}
	if err := window.Click("Open Resources & environments"); err != nil {
		t.Fatal(err)
	}
	if _, ok := window.Access("Session state is unknown on this device"); !ok {
		t.Error("returning to Resources lost the selected environment")
	}
}
