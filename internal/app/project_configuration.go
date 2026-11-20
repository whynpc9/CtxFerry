package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"

	"github.com/whynpc9/CtxFerry/internal/store"
)

// ProjectConfigurationTarget identifies the selected Project and an optional
// explicit local checkout binding. An empty binding never infers a checkout.
type ProjectConfigurationTarget struct {
	ProjectID  string
	CheckoutID string
}

type ProjectConfiguration struct {
	Profile         *SelectedProfile
	Profiles        []string
	CheckoutProblem string
	Fields          []ProjectConfigurationField
}

type ProjectConfigurationField struct {
	Name         string
	Layers       []EffectiveValue // directly stored values, from lowest precedence to highest
	Result       ProjectConfigurationValue
	WithoutLayer map[string]ProjectConfigurationValue
}

type ProjectConfigurationValue struct {
	Effective *EffectiveValue
	Problem   string
}

// ProjectConfigurationPreview is produced by the core and checked inside the
// write transaction. The selected context and the proposed change are pinned.
type ProjectConfigurationPreview struct {
	Target    ProjectConfigurationTarget
	Change    ConfigurationChange
	Before    ProjectConfigurationValue
	After     ProjectConfigurationValue
	Stored    any
	Checkouts []CheckoutConfigurationImpact
	token     [sha256.Size]byte
}

func projectConfigurationLocation(ctx context.Context, q rowQuerier, target ProjectConfigurationTarget) (configurationLocation, error) {
	var loc configurationLocation
	if target.ProjectID == "" {
		return loc, Fail("project-not-found", "choose a Project")
	}
	project, err := selectProject(ctx, q, target.ProjectID)
	if err != nil {
		return loc, err
	}
	if project.ID != target.ProjectID {
		return loc, Fail("project-not-found", "the selected Project is no longer in the catalog")
	}
	loc.project = project
	if err := requireSubscription(ctx, q, project.ID); err != nil {
		return loc, err
	}
	if err := q.QueryRowContext(ctx, "SELECT w.id,d.id FROM workspace w, device d WHERE w.singleton=1 AND d.singleton=1").Scan(&loc.workspace, &loc.device); err != nil {
		return loc, err
	}
	if target.CheckoutID != "" {
		checkout := CurrentCheckout{Origin: ConfigOrigin{Layer: layerCheckout, Source: "checkout-binding", ID: target.CheckoutID, Storage: "local-only"}}
		err := q.QueryRowContext(ctx, "SELECT path FROM checkout_bindings WHERE id=? AND project_id=? AND device_id=?", target.CheckoutID, project.ID, loc.device).Scan(&checkout.Path)
		if errors.Is(err, sql.ErrNoRows) {
			return loc, Fail("checkout-not-found", "the selected checkout is no longer bound on this device")
		}
		if err != nil {
			return loc, err
		}
		loc.checkout = &checkout
	}
	return loc, nil
}

func requireAvailableProjectCheckout(loc configurationLocation) error {
	path, err := requireCheckout(loc)
	if err != nil {
		return err
	}
	resolved, err := canonical(path)
	if err != nil || resolved != path {
		return Fail("checkout-unavailable", "the bound checkout directory is missing or now points to another directory")
	}
	return nil
}

func (a App) ReadProjectConfiguration(ctx context.Context, target ProjectConfigurationTarget) (ProjectConfiguration, error) {
	var out ProjectConfiguration
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	loc, err := projectConfigurationLocation(ctx, tx, target)
	if err != nil {
		return out, err
	}
	if err := requireAvailableProjectCheckout(loc); err != nil {
		out.CheckoutProblem = Classify(err).Code
	}
	out.Profiles, err = projectProfiles(ctx, tx, loc.project.ID)
	if err != nil {
		return out, err
	}
	out.Profile, err = selectedProfile(ctx, tx, loc)
	if err != nil {
		return out, err
	}
	layers, err := projectConfigurationValues(ctx, tx, loc, out.Profile)
	for _, field := range []string{"timeoutSeconds", "parallelism", "workingDirectory"} {
		values := layers[field]
		fallbacks := make(map[string]ProjectConfigurationValue, len(values))
		for _, value := range values {
			without := withoutConfigurationLayer(values, value.Origin.Layer)
			fallbacks[value.Origin.Layer] = effectiveProjectValue(loc, without)
		}
		out.Fields = append(out.Fields, ProjectConfigurationField{
			Name: field, Layers: values, Result: effectiveProjectValue(loc, values), WithoutLayer: fallbacks,
		})
	}
	return out, err
}

func projectProfiles(ctx context.Context, q projectReader, projectID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT cast(json_extract(payload,'$.profile') AS TEXT) AS name
		FROM configurations WHERE project_id=? AND name<>'' ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func projectConfigurationValues(ctx context.Context, q rowQuerier, loc configurationLocation, profile *SelectedProfile) (map[string][]EffectiveValue, error) {
	values := make(map[string][]EffectiveValue)
	err := readConfigurationLayers(ctx, q, loc, profile, func(config configuration, origin ConfigOrigin) error {
		origin.UpdatedAt, origin.Revision = config.UpdatedAt, config.Revision
		for field, raw := range config.Values {
			value, err := configValue(field, raw, origin.Storage == "local-only")
			if err != nil {
				return err
			}
			values[field] = append(values[field], EffectiveValue{field, value, origin})
		}
		return nil
	})
	return values, err
}

func withoutConfigurationLayer(values []EffectiveValue, layer string) []EffectiveValue {
	return slices.DeleteFunc(slices.Clone(values), func(value EffectiveValue) bool {
		return value.Origin.Layer == layer
	})
}

func effectiveProjectValue(loc configurationLocation, layers []EffectiveValue) ProjectConfigurationValue {
	if len(layers) == 0 {
		return ProjectConfigurationValue{}
	}
	value := layers[len(layers)-1]
	out := ProjectConfigurationValue{Effective: &value}
	if value.Field == "workingDirectory" {
		if !filepath.IsAbs(value.Value.(string)) && loc.checkout != nil {
			if err := requireAvailableProjectCheckout(loc); err != nil {
				out.Problem = Classify(err).Message
				return out
			}
		}
		path, err := configuredDirectory(loc, value)
		if err != nil {
			out.Problem = Classify(err).Message
		} else {
			value.Value = path
		}
	}
	return out
}

func (a App) PreviewProjectConfiguration(ctx context.Context, target ProjectConfigurationTarget, change ConfigurationChange) (ProjectConfigurationPreview, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return ProjectConfigurationPreview{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProjectConfigurationPreview{}, err
	}
	defer tx.Rollback()
	loc, err := projectConfigurationLocation(ctx, tx, target)
	if err != nil {
		return ProjectConfigurationPreview{}, err
	}
	return previewProjectConfiguration(ctx, tx, loc, target, change)
}

func previewProjectConfiguration(ctx context.Context, q projectReader, loc configurationLocation, target ProjectConfigurationTarget, change ConfigurationChange) (ProjectConfigurationPreview, error) {
	out := ProjectConfigurationPreview{Target: target, Change: change}
	if change.Create {
		return out, Fail("usage", "use the Profile creation operation")
	}
	profile, err := selectedProfile(ctx, q, loc)
	if err != nil {
		return out, err
	}
	if change.Layer == layerProfile && (profile == nil || change.Profile != profile.Name) {
		return out, Fail("profile-required", "select this Profile on the checkout before changing its configuration")
	}
	if change.Layer == layerCheckout {
		if err := requireAvailableProjectCheckout(loc); err != nil {
			return out, err
		}
	}
	if !slices.Contains(configurationOrder[:], change.Layer) {
		return out, Fail("usage", "choose a configuration layer")
	}
	values, err := projectConfigurationValues(ctx, q, loc, profile)
	if err != nil {
		return out, err
	}
	current := values[change.Field]
	out.Before = effectiveProjectValue(loc, current)
	if change.Value == nil && len(withoutConfigurationLayer(current, change.Layer)) == len(current) {
		return out, Fail("override-not-found", "this field has no value at the selected layer")
	}
	if change.Value != nil {
		out.Stored, err = configValue(change.Field, change.Value, localConfigurationLayer(change.Layer))
		if err != nil {
			return out, err
		}
	}
	out.After = projectConfigurationAfter(loc, current, change, out.Stored)
	out.Checkouts, err = configurationImpact(ctx, q, loc, out)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal([]any{loc.workspace, loc.device, loc.project.ID, loc.checkout, profile, current, change, out.Before, out.After, out.Checkouts})
	if err == nil {
		out.token = sha256.Sum256(raw)
	}
	return out, err
}

func (preview ProjectConfigurationPreview) verify(ctx context.Context, q projectReader) (configurationLocation, error) {
	loc, err := projectConfigurationLocation(ctx, q, preview.Target)
	if err != nil {
		return loc, err
	}
	fresh, err := previewProjectConfiguration(ctx, q, loc, preview.Target, preview.Change)
	if err != nil {
		return loc, err
	}
	if fresh.token != preview.token {
		return loc, Fail("configuration-changed", "the configuration or selected checkout changed; reopen the preview")
	}
	return loc, nil
}

func (a App) ApplyProjectConfiguration(ctx context.Context, preview ProjectConfigurationPreview) (ConfigurationReceipt, error) {
	if preview.token == ([sha256.Size]byte{}) {
		return ConfigurationReceipt{}, Fail("usage", "preview the configuration change first")
	}
	return a.changeConfiguration(ctx, "", "", preview.Change, preview.verify)
}

func (a App) CreateProjectProfile(ctx context.Context, projectID, name string) (ConfigurationReceipt, error) {
	// Profile creation is Project-scoped, independent of any checkout selection.
	return a.changeConfiguration(ctx, "", "", ConfigurationChange{
		Layer: layerProfile, Profile: name, Create: true,
	}, func(ctx context.Context, q projectReader) (configurationLocation, error) {
		return projectConfigurationLocation(ctx, q, ProjectConfigurationTarget{ProjectID: projectID})
	})
}
