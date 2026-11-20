package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectConfigurationPreviewProtectsScopeAndInheritance(t *testing.T) {
	ctx := t.Context()
	a := App{DataDir: t.TempDir()}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	one, err := a.BindProject(ctx, t.TempDir(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	project := one.Project.ID
	two, err := a.BindProjectWith(ctx, t.TempDir(), "", project, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := ProjectConfigurationTarget{ProjectID: project, CheckoutID: one.Binding.ID}
	second := ProjectConfigurationTarget{ProjectID: project, CheckoutID: two.Binding.ID}
	set := func(path, layer, profile, field, value string) {
		t.Helper()
		selector := project
		if layer == layerGlobal {
			selector = ""
		}
		if _, err := a.ChangeConfiguration(ctx, selector, path,
			ConfigurationChange{Layer: layer, Profile: profile, Field: field, Value: json.RawMessage(value)}); err != nil {
			t.Fatal(err)
		}
	}
	set(one.Binding.Path, layerGlobal, "", "timeoutSeconds", "30")
	set(one.Binding.Path, layerProject, "", "timeoutSeconds", "60")
	if _, err := a.CreateProjectProfile(ctx, project, "development"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SelectProjectCheckoutProfile(ctx, first, "development"); err != nil {
		t.Fatal(err)
	}
	set(one.Binding.Path, layerProfile, "development", "parallelism", "4")
	set(one.Binding.Path, layerDevice, "", "timeoutSeconds", "90")
	set(one.Binding.Path, layerDevice, "", "parallelism", "8")
	set(two.Binding.Path, layerCheckout, "", "timeoutSeconds", "120")
	set(one.Binding.Path, layerProject, "", "workingDirectory", `"missing"`)

	reset := ConfigurationChange{Layer: layerDevice, Field: "timeoutSeconds"}
	preview, err := a.PreviewProjectConfiguration(ctx, first, reset)
	if err != nil || preview.Before.Effective.Value != 90 || preview.After.Effective.Value != 60 {
		t.Fatalf("wrong inheritance preview: %#v, %v", preview, err)
	}
	set(one.Binding.Path, layerProject, "", "timeoutSeconds", "70")
	if _, err := a.ApplyProjectConfiguration(ctx, preview); err == nil || Classify(err).Code != "configuration-changed" {
		t.Fatalf("stale preview was accepted: %v", err)
	}
	preview, err = a.PreviewProjectConfiguration(ctx, first, reset)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.ApplyProjectConfiguration(ctx, preview); err != nil || r.Storage != "local-only" {
		t.Fatalf("reset failed: %#v, %v", r, err)
	}
	current, err := a.ReadProjectConfiguration(ctx, first)
	if err != nil || current.Profile.Name != "development" || current.Fields[0].Result.Effective.Value != 70 ||
		current.Fields[0].Result.Effective.Origin.Layer != layerProject || current.Fields[1].Result.Effective.Value != 8 ||
		current.Fields[2].Result.Problem == "" {
		t.Fatalf("wrong read-back on first checkout: %#v, %v", current, err)
	}
	current, err = a.ReadProjectConfiguration(ctx, second)
	if err != nil || current.Profile != nil || current.Fields[0].Result.Effective.Value != 120 {
		t.Fatalf("second checkout changed: %#v, %v", current, err)
	}

	preview, err = a.PreviewProjectConfiguration(ctx, second, ConfigurationChange{
		Layer: layerProject, Field: "timeoutSeconds", Value: json.RawMessage("55"),
	})
	if err != nil || preview.Stored != 55 || preview.After.Effective.Value != 120 || preview.After.Effective.Origin.Layer != layerCheckout {
		t.Fatalf("preview failed to retain higher override: %#v, %v", preview, err)
	}
	if err := os.Rename(two.Binding.Path, filepath.Join(filepath.Dir(two.Binding.Path), "moved")); err != nil {
		t.Fatal(err)
	}
	current, err = a.ReadProjectConfiguration(ctx, second)
	if err != nil || current.CheckoutProblem != "checkout-unavailable" {
		t.Fatalf("missing checkout not identified: %#v, %v", current, err)
	}
	if _, err := a.PreviewProjectConfiguration(ctx, second, ConfigurationChange{Layer: layerCheckout, Field: "timeoutSeconds"}); err == nil || Classify(err).Code != "checkout-unavailable" {
		t.Fatalf("missing checkout write preview permitted: %v", err)
	}
}
