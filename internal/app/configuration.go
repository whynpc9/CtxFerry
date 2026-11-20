package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

const (
	layerGlobal   = "global"
	layerProject  = "project"
	layerProfile  = "profile"
	layerDevice   = "device"
	layerCheckout = "checkout"
)

type configuration struct {
	SecretMappings   []SecretMapping            `json:"secretMappings,omitempty"`
	ResourceMappings []ResourceMapping          `json:"resourceMappings,omitempty"`
	Profile          string                     `json:"profile,omitempty"`
	Values           map[string]json.RawMessage `json:"values"`
	UpdatedAt        string                     `json:"updatedAt"`
	Revision         string                     `json:"revision"`
}

type ConfigOrigin struct {
	Layer     string `json:"layer"`
	Source    string `json:"source"`
	ID        string `json:"id,omitempty"`
	Storage   string `json:"storage"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Revision  string `json:"revision,omitempty"`
}

type EffectiveValue struct {
	Field  string       `json:"field"`
	Value  any          `json:"value"`
	Origin ConfigOrigin `json:"origin"`
}

type SelectedProfile struct {
	Name   string       `json:"name"`
	ID     string       `json:"id"`
	Origin ConfigOrigin `json:"origin"`
}

type CurrentCheckout struct {
	Path   string       `json:"path"`
	Origin ConfigOrigin `json:"origin"`
}

type ConfigurationExplanation struct {
	Project    Project          `json:"project"`
	DeviceID   string           `json:"deviceId"`
	Checkout   *CurrentCheckout `json:"checkout"`
	Profile    *SelectedProfile `json:"profile"`
	Values     []EffectiveValue `json:"values"`
	Source     string           `json:"source"`
	UpdatedAt  string           `json:"updatedAt"`
	ObservedAt *string          `json:"observedAt"`
	Revision   *string          `json:"revision"`
	Freshness  string           `json:"freshness"`
}

type configurationLocation struct {
	project   Project
	device    string
	workspace string
	checkout  *CurrentCheckout
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func configID(workspace string, scope Scope, profile string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("ctxferry/configuration/"+workspace+"/"+scope.Kind+"/"+scope.ID+"/"+profile)).String()
}

func profileName(name string) bool {
	return len(name) <= 63 && validSlug.MatchString(name)
}

func configValue(field string, raw json.RawMessage, local bool) (any, error) {
	switch field {
	case "parallelism", "timeoutSeconds":
		var value *int
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return nil, Fail("config-type-conflict", field+" requires a JSON integer")
		}
		maximum := 86400
		if field == "parallelism" {
			maximum = 1024
		}
		if *value < 1 || *value > maximum {
			return nil, Fail("invalid-config", field+" is out of range")
		}
		return *value, nil
	case "workingDirectory":
		var value *string
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return nil, Fail("config-type-conflict", field+" requires a JSON string")
		}
		if *value == "" || len(*value) > 4096 || strings.ContainsFunc(*value, unicode.IsControl) {
			return nil, Fail("invalid-config-path", "workingDirectory must be a directory path")
		}
		if !(local && filepath.IsAbs(*value)) && (!fs.ValidPath(*value) || strings.ContainsAny(*value, `\:`)) {
			return nil, Fail("invalid-config-path", "workingDirectory must be a checkout-relative slash path or a device-local absolute path")
		}
		return *value, nil
	default:
		return nil, Fail("unknown-config-field", "field is outside the ordinary configuration schema")
	}
}

func decodeConfiguration(raw []byte, local bool) (configuration, error) {
	var v configuration
	fields, err := configurationFields(raw)
	if err != nil {
		return v, err
	}
	for field := range fields {
		switch field {
		case "profile", "values", "updatedAt", "revision", "resourceMappings", "secretMappings":
		default:
			return v, Fail("unsupported-schema", "unknown configuration record field")
		}
	}
	if err := strictJSON(raw, &v); err != nil {
		return v, err
	}
	v.Values, err = configurationFields(fields["values"])
	if err != nil {
		return v, err
	}
	if !validUUID(v.Revision) || (fields["profile"] != nil && (local || !profileName(v.Profile))) {
		return v, Fail("invalid-config", "invalid configuration record")
	}
	if _, err := time.Parse(time.RFC3339Nano, v.UpdatedAt); err != nil {
		return v, Fail("invalid-config", "invalid configuration timestamp")
	}
	if fields["resourceMappings"] != nil {
		if local || v.Profile == "" || v.ResourceMappings == nil || len(v.ResourceMappings) > 512 {
			return v, Fail("invalid-config", "Resource mappings require a shared Profile")
		}
		seen := map[[2]string]bool{}
		for _, m := range v.ResourceMappings {
			key := [2]string{m.TaskID, m.Slot}
			if !validUUID(m.TaskID) || !validUUID(m.ResourceID) || !resourceToken.MatchString(m.Slot) || seen[key] {
				return v, Fail("invalid-config", "Resource mappings require distinct Task slots and valid IDs")
			}
			seen[key] = true
		}
	}
	if fields["secretMappings"] != nil {
		if local || v.Profile == "" || v.SecretMappings == nil || len(v.SecretMappings) > 512 {
			return v, Fail("invalid-config", "credential mappings require a shared Profile")
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(fields["secretMappings"], &entries); err != nil {
			return v, Fail("invalid-config", "credential mappings must be an array")
		}
		seen := map[[2]string]bool{}
		for i, m := range v.SecretMappings {
			if err := exactSecretFields(entries[i], "taskId", "requirement", "bindingId"); err != nil {
				return v, err
			}
			if !validUUID(m.TaskID) || !validUUID(m.BindingID) || !resourceToken.MatchString(m.Requirement) {
				return v, Fail("invalid-config", "credential mappings require a Task UUID, requirement name and binding UUID")
			}
			key := [2]string{m.TaskID, m.Requirement}
			if seen[key] {
				return v, Fail("binding-conflict", "multiple bindings for the same Task requirement")
			}
			seen[key] = true
		}
	}
	for field, value := range v.Values {
		if _, err := configValue(field, value, local); err != nil {
			return v, err
		}
	}
	return v, nil
}

// Decode object members before a map can silently collapse duplicate keys.
func configurationFields(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, Fail("invalid-config", "configuration requires a JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, Fail("invalid-config", "malformed configuration object")
		}
		key := token.(string)
		if _, exists := fields[key]; exists {
			return nil, Fail("ambiguous-config", "multiple candidates for a configuration field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, Fail("invalid-config", "malformed configuration value")
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, Fail("invalid-config", "malformed configuration object")
	}
	return fields, nil
}

func readConfiguration(row *sql.Row, local bool) (configuration, bool, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return configuration{Values: make(map[string]json.RawMessage)}, false, nil
		}
		return configuration{}, false, err
	}
	value, err := decodeConfiguration([]byte(raw), local)
	return value, true, err
}

func configLocation(ctx context.Context, db *sql.DB, selector, cwd string) (configurationLocation, error) {
	r, err := resolveProject(ctx, db, selector, cwd)
	if err != nil {
		return configurationLocation{}, err
	}
	return configurationAt(ctx, db, r)
}

func configurationAt(ctx context.Context, db *sql.DB, r Resolution) (configurationLocation, error) {
	loc := configurationLocation{project: *r.Project, device: r.Device.ID}
	if err := db.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&loc.workspace); err != nil {
		return loc, err
	}
	var subscribed int
	err := db.QueryRowContext(ctx, "SELECT subscribed FROM project_subscriptions WHERE project_id=?", loc.project.ID).Scan(&subscribed)
	if err == nil && subscribed == 0 {
		return loc, Fail("not-subscribed", "subscribe to this Project to access its configuration")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return loc, err
	}
	rows, err := db.QueryContext(ctx, `SELECT b.id,b.path,coalesce(b.git_common_dir,''),coalesce(m.subdirectory,''),coalesce(m.id,'')
		FROM checkout_bindings b LEFT JOIN memberships m ON m.id=b.membership_id
		WHERE b.device_id=? AND b.project_id=?`, loc.device, loc.project.ID)
	if err != nil {
		return loc, err
	}
	defer rows.Close()
	var direct, worktree *CurrentCheckout
	ambiguous := false
	for rows.Next() {
		var id, path, common, subdir, membership string
		if err := rows.Scan(&id, &path, &common, &subdir, &membership); err != nil {
			return loc, err
		}
		if contains(path, r.Directory) && (direct == nil || len(path) > len(direct.Path)) {
			direct = &CurrentCheckout{path, ConfigOrigin{Layer: layerCheckout, Source: "checkout-binding", ID: id, Storage: "local-only"}}
		}
		if common == "" || common != r.Git.CommonDir || subdir == "" {
			continue
		}
		root := filepath.Join(r.Git.CheckoutRoot, filepath.FromSlash(subdir))
		if contains(root, r.Directory) {
			if worktree == nil || len(root) > len(worktree.Path) {
				worktree = &CurrentCheckout{root, ConfigOrigin{Layer: layerCheckout, Source: "git-common-dir", ID: membership, Storage: "local-only"}}
				ambiguous = false
			} else if root == worktree.Path && membership != worktree.Origin.ID {
				ambiguous = true
			}
		}
	}
	if direct != nil {
		loc.checkout = direct
	} else if ambiguous {
		return loc, Fail("ambiguous-checkout", "multiple checkout candidates match the current directory")
	} else {
		loc.checkout = worktree
	}
	return loc, rows.Err()
}

func requireCheckout(loc configurationLocation) (string, error) {
	if loc.checkout == nil {
		return "", Fail("checkout-required", "run from a local checkout of this Project")
	}
	return loc.checkout.Path, nil
}

func selectedProfile(ctx context.Context, q rowQuerier, loc configurationLocation) (*SelectedProfile, error) {
	if loc.checkout == nil {
		return nil, nil
	}
	p := SelectedProfile{Origin: ConfigOrigin{Layer: layerCheckout, Source: "profile-selection", Storage: "local-only"}}
	err := q.QueryRowContext(ctx, `SELECT profile,selected_at FROM profile_selections WHERE device_id=? AND project_id=? AND checkout=?`,
		loc.device, loc.project.ID, loc.checkout.Path).Scan(&p.Name, &p.Origin.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.ID = configID(loc.workspace, Scope{"project", loc.project.ID}, p.Name)
	return &p, nil
}

func chooseProfile(ctx context.Context, q rowQuerier, loc configurationLocation, name string) (*SelectedProfile, error) {
	if name == "" {
		return selectedProfile(ctx, q, loc)
	}
	if !profileName(name) {
		return nil, Fail("invalid-profile", "Profile name must be a slug of up to 63 characters")
	}
	return &SelectedProfile{
		Name: name, ID: configID(loc.workspace, Scope{"project", loc.project.ID}, name),
		Origin: ConfigOrigin{Layer: layerProfile, Source: "explicit", Storage: "local-only"},
	}, nil
}

func (a App) ExplainConfiguration(ctx context.Context, selector, cwd, name string) (ConfigurationExplanation, error) {
	out := ConfigurationExplanation{Source: "local-store", Freshness: "unknown", Values: []EffectiveValue{}}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	loc, err := configLocation(ctx, db, selector, cwd)
	if err != nil {
		return out, err
	}
	return explainConfigurationAt(ctx, db, loc, name)
}

func explainConfigurationAt(ctx context.Context, db *sql.DB, loc configurationLocation, name string) (ConfigurationExplanation, error) {
	out := ConfigurationExplanation{Source: "local-store", Freshness: "unknown", Values: []EffectiveValue{}}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.Project, out.DeviceID, out.Checkout, out.UpdatedAt = loc.project, loc.device, loc.checkout, loc.project.CreatedAt
	out.Profile, err = chooseProfile(ctx, tx, loc, name)
	if err != nil {
		return out, err
	}

	values := map[string]EffectiveValue{}
	latest, _ := time.Parse(time.RFC3339Nano, out.UpdatedAt)
	updateTimestamp := func(stamp string) {
		if candidate, err := time.Parse(time.RFC3339Nano, stamp); err == nil && candidate.After(latest) {
			latest, out.UpdatedAt = candidate, stamp
		}
	}
	apply := func(v configuration, origin ConfigOrigin) error {
		origin.UpdatedAt, origin.Revision = v.UpdatedAt, v.Revision
		updateTimestamp(v.UpdatedAt)
		for field, raw := range v.Values {
			value, err := configValue(field, raw, origin.Storage == "local-only")
			if err != nil {
				return err
			}
			values[field] = EffectiveValue{field, value, origin}
		}
		return nil
	}
	if out.Profile != nil {
		updateTimestamp(out.Profile.Origin.UpdatedAt)
	}
	if err := readConfigurationLayers(ctx, tx, loc, out.Profile, apply); err != nil {
		return out, err
	}
	if entry, ok := values["workingDirectory"]; ok {
		path, err := configuredDirectory(loc, entry)
		if err != nil {
			return out, err
		}
		entry.Value = path
		values[entry.Field] = entry
	}
	for _, value := range values {
		out.Values = append(out.Values, value)
	}
	sort.Slice(out.Values, func(i, j int) bool { return out.Values[i].Field < out.Values[j].Field })
	return out, nil
}

// readConfigurationLayers visits configured layers in increasing precedence.
func readConfigurationLayers(ctx context.Context, q rowQuerier, loc configurationLocation, profile *SelectedProfile, apply func(configuration, ConfigOrigin) error) error {
	project := Scope{"project", loc.project.ID}
	shared := []struct{ layer, id string }{
		{layerGlobal, configID(loc.workspace, Scope{"workspace", loc.workspace}, "")},
		{layerProject, configID(loc.workspace, project, "")},
	}
	if profile != nil {
		shared = append(shared, struct{ layer, id string }{layerProfile, profile.ID})
	}
	for _, layer := range shared {
		if err := requireNoConflict(ctx, q, layer.id); err != nil {
			return err
		}
		v, found, err := readConfiguration(q.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", layer.id), false)
		if err != nil {
			return err
		}
		if !found {
			if layer.layer == layerProfile {
				return Fail("profile-not-found", "Profile was not found in this Project")
			}
			continue
		}
		if err := apply(v, ConfigOrigin{Layer: layer.layer, Source: "configuration", ID: layer.id, Storage: "shared"}); err != nil {
			return err
		}
	}
	return applyLocalConfiguration(ctx, q, loc, apply)
}

func applyLocalConfiguration(ctx context.Context, q rowQuerier, loc configurationLocation, apply func(configuration, ConfigOrigin) error) error {
	scopes := []struct{ layer, checkout, id string }{{layerDevice, "", loc.device}}
	if loc.checkout != nil {
		scopes = append(scopes, struct{ layer, checkout, id string }{layerCheckout, loc.checkout.Path, loc.checkout.Origin.ID})
	}
	for _, scope := range scopes {
		v, found, err := readConfiguration(q.QueryRowContext(ctx, `SELECT payload FROM local_configurations WHERE device_id=? AND project_id=? AND checkout=?`,
			loc.device, loc.project.ID, scope.checkout), true)
		if err != nil {
			return err
		}
		if found {
			if err := apply(v, ConfigOrigin{Layer: scope.layer, Source: "configuration", ID: scope.id, Storage: "local-only"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func configuredDirectory(loc configurationLocation, entry EffectiveValue) (string, error) {
	path := entry.Value.(string)
	local := entry.Origin.Storage == "local-only"
	if !filepath.IsAbs(path) {
		root, err := requireCheckout(loc)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Localize(path)
		if err != nil {
			return "", Fail("invalid-config-path", "workingDirectory is not valid on this device")
		}
		path = filepath.Join(root, rel)
	}
	resolved, err := canonical(path)
	if err != nil {
		return "", Fail("invalid-config-path", "effective workingDirectory must exist and be a directory on this device")
	}
	if !local && !contains(loc.checkout.Path, resolved) {
		return "", Fail("invalid-config-path", "shared workingDirectory must resolve within the checkout")
	}
	return resolved, nil
}
