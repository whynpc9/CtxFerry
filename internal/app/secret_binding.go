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
	BindingID       string
	Alias           string
	ProviderProfile provider.Profile
	Locator         provider.Locator
}

type SecretBindResult struct {
	Profile string        `json:"profile"`
	Binding SecretBinding `json:"binding"`
	Mapping SecretMapping `json:"mapping"`
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
	if !resourceToken.MatchString(s.Alias) || !validUUID(s.ResourceID) || !taskText(s.Purpose) || !taskText(s.Type) ||
		!validProviderProfile(s.ProviderProfile) || !validLocator(s.Locator) {
		return Fail("invalid-secret-binding", "a binding requires an alias, Resource UUID, purpose, type, provider profile and one exact locator")
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

func compatibleSecret(req CredentialRequirement, resourceID string, binding SecretBindingSpec) bool {
	return resourceID != "" && resourceID == binding.ResourceID && req.Purpose == binding.Purpose && req.Type == binding.Type
}

func secretBindingFromRecord(r remoteRecord) SecretBinding {
	return SecretBinding{r.SecretBinding, r.ID, r.Scope, r.Revision, r.Provenance.UpdatedAt}
}

const liveSecretBindings = ` FROM records WHERE kind='secret-binding' AND coalesce(json_extract(metadata,'$.deleted'),0)=0
	AND NOT EXISTS (SELECT 1 FROM sync_tombstones t WHERE t.id IN (records.id, records.project_id))`

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
	p, err := chooseProfile(ctx, q, loc, name)
	if err != nil {
		return nil, configuration{}, err
	}
	if p == nil {
		return nil, configuration{}, Fail("profile-required", "select a Profile or pass --profile")
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

func secretResource(ctx context.Context, q rowQuerier, cfg configuration, task Task, req CredentialRequirement) (Resource, error) {
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
	var out SecretBindResult
	reuse := change.BindingID != ""
	if reuse && (!validUUID(change.BindingID) || change.Alias != "" || change.ProviderProfile != (provider.Profile{}) || change.Locator != (provider.Locator{})) {
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
	if err := a.prepareBaseline(ctx, db); err != nil {
		return out, err
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		p, cfg, err := secretProfile(ctx, c, loc, change.Profile)
		if err != nil {
			return err
		}
		task, err := loadTask(ctx, c, loc.project.ID, change.Task)
		if err != nil {
			return err
		}
		req, ok := task.requirement(change.Requirement)
		if !ok {
			return Fail("incompatible", "Task has no credential requirement with that name")
		}
		if err := requireNoBindingConflict(ctx, c, task.ID); err != nil {
			return err
		}
		resource, err := secretResource(ctx, c, cfg, task, req)
		if err != nil {
			return err
		}
		scope := Scope{"project", loc.project.ID}
		i := slices.IndexFunc(cfg.SecretMappings, func(m SecretMapping) bool { return m.TaskID == task.ID && m.Requirement == req.Name })
		var binding SecretBinding
		if reuse {
			if err := requireNoBindingConflict(ctx, c, change.BindingID); err != nil {
				return err
			}
			binding, err = loadSecretBinding(ctx, c, change.BindingID)
			if err != nil {
				return err
			}
			if err := requireNoBindingConflict(ctx, c, binding.Scope.ID); err != nil {
				return err
			}
			if !compatibleSecret(req, resource.ID, binding.SecretBindingSpec) {
				return Fail("incompatible", "SecretBinding does not match the requirement purpose, type and selected Resource")
			}
		} else {
			spec := SecretBindingSpec{change.Alias, resource.ID, req.Purpose, req.Type, change.ProviderProfile, change.Locator}
			if err := validateSecretBinding(spec); err != nil {
				return err
			}
			if i >= 0 {
				current, err := loadSecretBinding(ctx, c, cfg.SecretMappings[i].BindingID)
				if err != nil {
					return err
				}
				if current.Scope == scope && current.SecretBindingSpec == spec {
					if err := requireNoBindingConflict(ctx, c, current.ID); err != nil {
						return err
					}
					out = SecretBindResult{p.Name, current, cfg.SecretMappings[i]}
					return nil
				}
			}
			env := publishedEnvelope{SchemaVersion: 1, ID: uuid.NewString(), Kind: secretBindingKind,
				WorkspaceID: loc.workspace, Scope: scope, Policy: Policy{"shared", "reference"},
				Revision: uuid.NewString(), OperationID: uuid.NewString(), Payload: spec}
			env.Provenance.DeviceID, env.Provenance.UpdatedAt = loc.device, now()
			raw, err := json.Marshal(env)
			if err != nil {
				return err
			}
			if _, err := c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?)", env.ID, scope.ID, env.Kind, string(raw)); err != nil {
				return err
			}
			binding = secretBindingFromRecord(remoteRecord{publishedEnvelope: env, SecretBinding: spec})
		}

		mapping := SecretMapping{task.ID, req.Name, binding.ID}
		out = SecretBindResult{p.Name, binding, mapping}
		if i < 0 {
			cfg.SecretMappings = append(cfg.SecretMappings, mapping)
		} else if cfg.SecretMappings[i] == mapping {
			return nil
		} else {
			cfg.SecretMappings[i] = mapping
		}
		if len(cfg.SecretMappings) > 512 {
			return Fail("size-limit", "a Profile supports at most 512 credential mappings")
		}
		cfg.UpdatedAt, cfg.Revision = now(), uuid.NewString()
		if err := saveConfiguration(ctx, c, p.ID, scope, cfg); err != nil {
			return err
		}
		return queueChanges(ctx, c)
	})
	return out, err
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
