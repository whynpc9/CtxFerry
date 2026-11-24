package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/bitwarden"
	"github.com/whynpc9/CtxFerry/internal/store"
)

const secretBindingKind = "secret-binding"

type SecretBindingSpec struct {
	Alias           string           `json:"alias"`
	ResourceID      string           `json:"resourceId"`
	Purpose         string           `json:"purpose"`
	Type            string           `json:"type"`
	ProviderProfile provider.Profile `json:"providerProfile"`
	Locator         provider.Locator `json:"locator"`
}

type SecretBinding struct {
	SecretBindingSpec
	ID        string `json:"id"`
	Scope     Scope  `json:"scope"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

type SecretMapping struct {
	TaskID      string `json:"taskId"`
	Requirement string `json:"requirement"`
	BindingID   string `json:"bindingId"`
}

type SecretTarget struct {
	Profile     string
	Task        string
	Requirement string
}

type SecretBindingChange struct {
	SecretTarget
	BindingID string
	// Like names a binding, by alias or UUID, whose provider profile and
	// backend namespace a new binding copies where it gives none of its own.
	Like            string
	Alias           string
	ProviderProfile provider.Profile
	Locator         provider.Locator
}

type SecretBindResult struct {
	Profile      string         `json:"profile"`
	Binding      *SecretBinding `json:"binding,omitempty"`
	Mapping      SecretMapping  `json:"mapping"`
	desktopWrite string
}

func validProviderProfile(p provider.Profile) bool {
	return resourceToken.MatchString(p.Provider) && resourceToken.MatchString(p.Name) && resourceToken.MatchString(p.Method)
}

func validLocator(l provider.Locator) bool {
	if l.EntryID != "" {
		return taskText(l.EntryID) && l == (provider.Locator{EntryID: l.EntryID})
	}
	path := strings.TrimPrefix(l.Path, "/")
	return taskText(l.ProjectID) && taskText(l.Environment) && taskText(l.Key) &&
		strings.HasPrefix(l.Path, "/") && (l.Path == "/" || path != "." && taskPath(path))
}

func validateSecretBinding(s SecretBindingSpec) error {
	if !resourceToken.MatchString(s.Alias) || s.ResourceID != "" && !validUUID(s.ResourceID) || !taskText(s.Purpose) || !taskText(s.Type) ||
		!validProviderProfile(s.ProviderProfile) || !validLocator(s.Locator) {
		return Fail("invalid-secret-binding", "a binding requires an alias, an optional Resource UUID, purpose, type, provider profile and one exact locator")
	}
	return nil
}

func (spec TaskSpec) requirement(name string) (CredentialRequirement, bool) {
	for _, req := range spec.CredentialRequirements {
		if req.Name == name {
			return req, true
		}
	}
	return CredentialRequirement{}, false
}

// compatibleSecret keeps standalone and slot-attached credentials apart: a
// standalone requirement takes only a binding without a Resource, and a
// slot-attached one only a binding for the Resource its slot is mapped to.
func compatibleSecret(req CredentialRequirement, resourceID string, binding SecretBindingSpec) bool {
	if req.Purpose != binding.Purpose || req.Type != binding.Type {
		return false
	}
	if req.standalone() {
		return binding.ResourceID == ""
	}
	return resourceID != "" && resourceID == binding.ResourceID
}

// standalone reports a requirement that no Resource slot backs.
func (req CredentialRequirement) standalone() bool { return req.ResourceSlot == "" }

func secretBindingFromRecord(r remoteRecord) SecretBinding {
	return SecretBinding{r.SecretBinding, r.ID, r.Scope, r.Revision, r.Provenance.UpdatedAt}
}

var liveSecretBindings = ` FROM records WHERE kind='secret-binding' AND coalesce(json_extract(metadata,'$.deleted'),0)=0
	AND ` + live("records.id", "records.project_id")

func secretBindingRecord(raw []byte) (remoteRecord, error) {
	r, err := decodeRecord(raw)
	if err != nil {
		return r, err
	}
	if r.Kind != secretBindingKind || r.Scope.Kind != "project" || r.Policy != (Policy{"shared", "reference"}) {
		return r, Fail("invalid-secret-binding", "invalid SecretBinding scope or policy")
	}
	return r, validateSecretBinding(r.SecretBinding)
}

func loadSecretBinding(ctx context.Context, q rowQuerier, id string) (SecretBinding, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, "SELECT metadata"+liveSecretBindings+" AND id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretBinding{}, Fail("binding-missing", "mapped SecretBinding is unavailable")
	}
	if err != nil {
		return SecretBinding{}, err
	}
	r, err := secretBindingRecord(raw)
	return secretBindingFromRecord(r), err
}

func requireNoBindingConflict(ctx context.Context, q rowQuerier, ids ...string) error {
	for _, id := range ids {
		if err := requireNoConflict(ctx, q, id); err != nil {
			if Classify(err).Code == "conflict-unresolved" {
				return Fail("binding-conflict", "resolve the catalog conflict before using this mapping")
			}
			return err
		}
	}
	return nil
}

func secretProfile(ctx context.Context, q rowQuerier, loc configurationLocation, name string) (*SelectedProfile, configuration, error) {
	p, err := executionProfile(ctx, q, loc, name)
	if err != nil {
		return nil, configuration{}, err
	}
	if p == nil {
		return nil, configuration{}, Fail("profile-required", "select a Profile, pass --profile or create a Profile named default")
	}
	if err := requireNoBindingConflict(ctx, q, loc.project.ID, p.ID); err != nil {
		return p, configuration{}, err
	}
	cfg, exists, err := readConfiguration(q.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", p.ID), false)
	if err == nil && !exists {
		err = Fail("profile-not-found", "Profile was not found in this Project")
	}
	return p, cfg, err
}

// secretResource returns the Resource the Profile maps to the requirement's
// slot. A standalone requirement has none and gets the zero Resource.
func secretResource(ctx context.Context, q rowQuerier, cfg configuration, task Task, req CredentialRequirement) (Resource, error) {
	if req.standalone() {
		if task.Policy.Storage != "shared" {
			return Resource{}, Fail("incompatible", "a credential mapping requires a shared Task")
		}
		return Resource{}, nil
	}
	id, err := mappedResourceID(cfg, task, req.ResourceSlot)
	if err != nil {
		return Resource{}, err
	}
	if err := requireNoBindingConflict(ctx, q, id); err != nil {
		return Resource{}, err
	}
	resource, err := loadMappedResource(ctx, q, id)
	if err != nil {
		if Classify(err).Code == "resource-not-found" {
			return Resource{}, Fail("binding-missing", "mapped Resource is unavailable")
		}
		return Resource{}, err
	}
	if err := requireNoBindingConflict(ctx, q, resource.Scope.ID); err != nil {
		return Resource{}, err
	}
	if task.Policy.Storage != "shared" || resource.Policy.Storage != "shared" || !compatibleResource(task.TaskSpec, req.ResourceSlot, resource.ResourceSpec) {
		return Resource{}, Fail("incompatible", "Task and Resource must be shared and the Resource must match the slot kind and stage")
	}
	return resource, nil
}

func (a App) BindSecret(ctx context.Context, selector, cwd string, change SecretBindingChange) (SecretBindResult, error) {
	return a.changeSecretMapping(ctx, selector, cwd, change, false, nil)
}

func (a App) UnmapSecret(ctx context.Context, selector, cwd string, target SecretTarget) (SecretBindResult, error) {
	return a.changeSecretMapping(ctx, selector, cwd, SecretBindingChange{SecretTarget: target}, true, nil)
}

func (a App) changeSecretMapping(ctx context.Context, selector, cwd string, change SecretBindingChange, unmap bool, expected *DesktopCredentialSlot) (SecretBindResult, error) {
	var out SecretBindResult
	reuse := change.BindingID != ""
	if reuse && (!validUUID(change.BindingID) || change.Like != "" || change.Alias != "" || change.ProviderProfile != (provider.Profile{}) || change.Locator != (provider.Locator{})) {
		return out, Fail("usage", "pass an existing --binding UUID or specify new binding metadata")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	loc, err := configLocation(ctx, db, selector, cwd)
	if err != nil {
		return out, err
	}
	var queueCheck []string
	err = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		inserted := false
		if !unmap && change.Profile == "" && expected == nil {
			created, err := ensureDefaultProfile(ctx, c, loc)
			if err != nil {
				return false, err
			}
			inserted = created
		}
		p, cfg, err := secretProfile(ctx, c, loc, change.Profile)
		if err != nil {
			return false, err
		}
		task, err := loadTask(ctx, c, loc.project.ID, change.Task)
		if err != nil {
			return false, err
		}
		req, ok := task.requirement(change.Requirement)
		if !ok {
			return false, Fail("incompatible", "Task has no credential requirement with that name")
		}
		if err := requireNoBindingConflict(ctx, c, task.ID); err != nil {
			return false, err
		}
		scope := Scope{"project", loc.project.ID}
		i := slices.IndexFunc(cfg.SecretMappings, func(m SecretMapping) bool { return m.TaskID == task.ID && m.Requirement == req.Name })
		if unmap {
			if i < 0 {
				return false, Fail("binding-missing", "Profile has no SecretBinding mapping for Task "+task.Name+" requirement "+req.Name)
			}
			out = SecretBindResult{Profile: p.Name, Mapping: cfg.SecretMappings[i]}
			cfg.SecretMappings = slices.Delete(cfg.SecretMappings, i, i+1)
			return true, saveSecretMappings(ctx, c, p.ID, scope, cfg)
		}

		resource, err := secretResource(ctx, c, cfg, task, req)
		if err != nil {
			return false, err
		}
		if expected != nil && (expected.workspace != loc.workspace || expected.Status.DeviceID != loc.device ||
			expected.profileRevision != cfg.Revision || expected.taskRevision != task.Revision ||
			expected.Resource.ID != resource.ID || expected.Resource.Revision != resource.Revision) {
			return false, Fail("selection-changed", "the shared mapping scope changed; refresh and confirm again")
		}
		if expected != nil && expected.Status.Binding != nil {
			previous := expected.Status.Binding
			if i < 0 || cfg.SecretMappings[i].BindingID != previous.ID {
				return false, Fail("selection-changed", "the previous binding changed; refresh and confirm again")
			}
			if err := requireNoBindingConflict(ctx, c, previous.ID, previous.Scope.ID); err != nil {
				return false, err
			}
			current, err := loadSecretBinding(ctx, c, previous.ID)
			if err != nil {
				return false, err
			}
			if current.Revision != previous.Revision {
				return false, Fail("selection-changed", "the previous binding changed; refresh and confirm again")
			}
		}
		var binding SecretBinding
		if reuse {
			if err := requireNoBindingConflict(ctx, c, change.BindingID); err != nil {
				return false, err
			}
			binding, err = loadSecretBinding(ctx, c, change.BindingID)
			if err != nil {
				return false, err
			}
			if err := requireNoBindingConflict(ctx, c, binding.Scope.ID); err != nil {
				return false, err
			}
			if expected != nil && !slices.ContainsFunc(expected.Reusable, func(b SecretBinding) bool {
				return b.ID == binding.ID && b.Revision == binding.Revision
			}) {
				return false, Fail("selection-changed", "the reusable binding changed; refresh and confirm again")
			}
			if !compatibleSecret(req, resource.ID, binding.SecretBindingSpec) {
				return false, incompatibleBinding(req)
			}
		} else {
			spec, err := newBindingSpec(ctx, c, loc.project, req, resource.ID, change)
			if err != nil {
				return false, err
			}
			if i >= 0 {
				current, err := loadSecretBinding(ctx, c, cfg.SecretMappings[i].BindingID)
				if err != nil && Classify(err).Code != "binding-missing" {
					return false, err
				}
				if err == nil && current.Scope == scope && current.SecretBindingSpec == spec {
					if err := requireNoBindingConflict(ctx, c, current.ID); err != nil {
						return false, err
					}
					out = SecretBindResult{Profile: p.Name, Binding: &current, Mapping: cfg.SecretMappings[i]}
					return inserted, nil
				}
			}
			binding, err = insertSecretBinding(ctx, c, loc, spec)
			if err != nil {
				return false, err
			}
			inserted = true
		}

		mapping := SecretMapping{task.ID, req.Name, binding.ID}
		out = SecretBindResult{Profile: p.Name, Binding: &binding, Mapping: mapping}
		if i < 0 {
			cfg.SecretMappings = append(cfg.SecretMappings, mapping)
		} else if cfg.SecretMappings[i] == mapping {
			return inserted, nil
		} else {
			cfg.SecretMappings[i] = mapping
		}
		if len(cfg.SecretMappings) > 512 {
			return false, Fail("size-limit", "a Profile supports at most 512 credential mappings")
		}
		if expected != nil {
			queueCheck = []string{p.ID, binding.ID}
		}
		return true, saveSecretMappings(ctx, c, p.ID, scope, cfg)
	})
	if err == nil && queueCheck != nil {
		// The desktop reports whether the confirmed mapping reached the sync outbox.
		var queued bool
		if err = db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM sync_outbox WHERE id IN (?,?))", queueCheck[0], queueCheck[1]).Scan(&queued); err != nil {
			return out, err
		}
		out.desktopWrite = "local"
		if queued {
			out.desktopWrite = "queued"
		}
	}
	return out, err
}

// ensureDefaultProfile creates the Project's default Profile when nothing
// selects a Profile here and the Project has none at all, so a
// single-environment Project needs no profile create and select. It reports
// whether it created one.
func ensureDefaultProfile(ctx context.Context, c *sql.Conn, loc configurationLocation) (bool, error) {
	p, cfg, err := pendingDefaultProfile(ctx, c, loc)
	if err != nil || p == nil {
		return false, err
	}
	return true, saveConfiguration(ctx, c, p.ID, Scope{"project", loc.project.ID}, cfg)
}

// pendingDefaultProfile returns the default Profile that a credential command
// without --profile would create, or nil when a Profile is selected or the
// Project already has Profiles.
func pendingDefaultProfile(ctx context.Context, q rowQuerier, loc configurationLocation) (*SelectedProfile, configuration, error) {
	p, err := chooseProfile(ctx, q, loc, "")
	if err != nil || p != nil {
		return nil, configuration{}, err
	}
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM configurations WHERE project_id=?
		AND coalesce(json_extract(payload,'$.profile'),'')<>'')`, loc.project.ID).Scan(&exists); err != nil || exists {
		return nil, configuration{}, err
	}
	id := configID(loc.workspace, Scope{"project", loc.project.ID}, defaultProfile)
	if err := requireNoConflict(ctx, q, id); err != nil {
		return nil, configuration{}, err
	}
	cfg := configuration{Values: map[string]json.RawMessage{}}
	ConfigurationChange{Layer: layerProfile, Profile: defaultProfile, Create: true}.apply(&cfg)
	return &SelectedProfile{Name: defaultProfile, ID: id,
		Origin: ConfigOrigin{Layer: layerProfile, Source: "default", Storage: "local-only"}}, cfg, nil
}

// newBindingSpec completes the metadata of a new binding. Fields given
// explicitly win; the rest come from the --like template binding, then from
// the requirement: its name for the alias and its delivery variable for the
// key, with the backend root as the path. The template's key or entry ID is
// never copied, since it names another entry.
func newBindingSpec(ctx context.Context, q projectReader, project Project, req CredentialRequirement, resourceID string, change SecretBindingChange) (SecretBindingSpec, error) {
	p, loc := change.ProviderProfile, change.Locator
	if change.Like != "" {
		like, err := templateBinding(ctx, q, project, change.Like)
		if err != nil {
			return SecretBindingSpec{}, err
		}
		fill := func(dst *string, src string) {
			if *dst == "" {
				*dst = src
			}
		}
		fill(&p.Provider, like.ProviderProfile.Provider)
		fill(&p.Name, like.ProviderProfile.Name)
		fill(&p.Method, like.ProviderProfile.Method)
		if like.Locator.EntryID == "" && loc.EntryID == "" {
			fill(&loc.ProjectID, like.Locator.ProjectID)
			fill(&loc.Environment, like.Locator.Environment)
			fill(&loc.Path, like.Locator.Path)
		}
	}
	alias := change.Alias
	if alias == "" {
		alias = req.Name
	}
	if loc.EntryID == "" && p.Provider != bitwarden.Name {
		if loc.Path == "" {
			loc.Path = "/"
		}
		if loc.Key == "" {
			loc.Key = req.Env
		}
	}
	spec := SecretBindingSpec{alias, resourceID, req.Purpose, req.Type, p, loc}
	if err := validateSecretBinding(spec); err != nil {
		return spec, err
	}
	if spec.ProviderProfile.Provider == bitwarden.Name && !bitwarden.ValidLocator(spec.Locator) {
		return spec, Fail("invalid-secret-binding", "Bitwarden requires an exact item UUID in --entry-id")
	}
	return spec, nil
}

// templateBinding finds the binding --like names among those the Project can
// see, by UUID or by an alias that only one of them has.
func templateBinding(ctx context.Context, q projectReader, project Project, ref string) (SecretBinding, error) {
	list, err := listProjectSecrets(ctx, q, project)
	if err != nil {
		return SecretBinding{}, err
	}
	var found []SecretBinding
	for _, b := range list.Bindings {
		if b.ID == ref || b.Alias == ref {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 0:
		return SecretBinding{}, Fail("binding-not-found", "no SecretBinding visible to this Project has that alias or UUID")
	case 1:
		return found[0], nil
	default:
		return SecretBinding{}, Fail("ambiguous-binding", "several SecretBindings have that alias; pass --like with a UUID from secret list")
	}
}

// incompatibleBinding explains why a binding cannot serve a requirement.
func incompatibleBinding(req CredentialRequirement) error {
	if req.standalone() {
		return Fail("incompatible", "SecretBinding does not match the standalone requirement: it needs the same purpose and type and no Resource")
	}
	return Fail("incompatible", "SecretBinding does not match the requirement purpose, type and selected Resource")
}

func insertSecretBinding(ctx context.Context, c *sql.Conn, loc configurationLocation, spec SecretBindingSpec) (SecretBinding, error) {
	scope := Scope{"project", loc.project.ID}
	env := publishedEnvelope{SchemaVersion: 1, ID: uuid.NewString(), Kind: secretBindingKind,
		WorkspaceID: loc.workspace, Scope: scope, Policy: Policy{"shared", "reference"},
		Revision: uuid.NewString(), OperationID: uuid.NewString(), Payload: spec}
	env.Provenance.DeviceID, env.Provenance.UpdatedAt = loc.device, now()
	raw, err := json.Marshal(env)
	if err != nil {
		return SecretBinding{}, err
	}
	if _, err := c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?)", env.ID, scope.ID, env.Kind, string(raw)); err != nil {
		return SecretBinding{}, err
	}
	return secretBindingFromRecord(remoteRecord{publishedEnvelope: env, SecretBinding: spec}), nil
}

func saveSecretMappings(ctx context.Context, c *sql.Conn, profileID string, scope Scope, cfg configuration) error {
	cfg.UpdatedAt, cfg.Revision = now(), uuid.NewString()
	return saveConfiguration(ctx, c, profileID, scope, cfg)
}

func exactSecretFields(raw []byte, allowed ...string) error {
	fields, err := configurationFields(raw)
	if err != nil {
		if Classify(err).Code == "ambiguous-config" {
			return Fail("binding-conflict", "duplicate fields in credential metadata")
		}
		return Fail("invalid-secret-binding", "credential metadata must be a JSON object")
	}
	for field := range fields {
		if !slices.Contains(allowed, field) {
			return Fail("unsupported-schema", "unknown credential metadata field")
		}
	}
	return nil
}

func decodeSecretBinding(raw []byte, spec *SecretBindingSpec) error {
	if err := exactSecretFields(raw, "alias", "resourceId", "purpose", "type", "providerProfile", "locator"); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := strictJSON(raw, &fields); err != nil {
		return err
	}
	if err := exactSecretFields(fields["providerProfile"], "provider", "name", "method"); err != nil {
		return err
	}
	if err := exactSecretFields(fields["locator"], "entryId", "projectId", "environment", "path", "key"); err != nil {
		return err
	}
	return strictJSON(raw, spec)
}
