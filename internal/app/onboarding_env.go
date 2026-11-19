package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type EnvCandidate struct {
	TaskID       string                  `json:"taskId"`
	Task         string                  `json:"task"`
	Requirements []CredentialRequirement `json:"requirements"`
}

type EnvScan struct {
	ProjectID  string         `json:"projectId"`
	Fields     []string       `json:"fields"`
	Candidates []EnvCandidate `json:"candidates"`
}

func (a App) ScanOnboardingEnv(ctx context.Context, selector, path string) (EnvScan, error) {
	var out EnvScan
	if selector == "" {
		return out, Fail("usage", "choose a Project with --project")
	}
	file, err := readEnvFile(path)
	if err != nil {
		return out, err
	}
	defer clear(file.body)
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	project, err := selectProject(ctx, db, selector)
	if err != nil {
		return out, err
	}
	if err := requireSubscription(ctx, db, project.ID); err != nil {
		return out, err
	}
	tasks, err := readTasks(ctx, db, project.ID, "")
	if err != nil {
		return out, err
	}
	out = EnvScan{ProjectID: project.ID, Fields: file.names, Candidates: []EnvCandidate{}}
	for _, task := range tasks {
		if task.Policy.Storage != "shared" {
			continue
		}
		candidate := EnvCandidate{TaskID: task.ID, Task: task.Name}
		for _, req := range task.CredentialRequirements {
			if _, exists := file.values[req.Env]; exists {
				candidate.Requirements = append(candidate.Requirements, req)
			}
		}
		if len(candidate.Requirements) > 0 {
			out.Candidates = append(out.Candidates, candidate)
		}
	}
	return out, nil
}

type EnvImport struct {
	SecretTarget
	Field           string
	ResourceID      string
	Alias           string
	ProviderProfile provider.Profile
	Locator         provider.Locator
	Recover         bool
	Confirm         string
}

type EnvImportPlan struct {
	Preview         string                `json:"preview"`
	ProjectID       string                `json:"projectId"`
	Profile         string                `json:"profile"`
	TaskID          string                `json:"taskId"`
	Task            string                `json:"task"`
	Field           string                `json:"field"`
	Requirement     CredentialRequirement `json:"requirement"`
	ResourcePurpose string                `json:"resourcePurpose"`
	Binding         SecretBindingSpec     `json:"binding"`
	Recover         bool                  `json:"recover"`
}

type EnvImportReceipt struct {
	ProjectID string          `json:"projectId"`
	Field     string          `json:"field"`
	Resource  ResourceMapping `json:"resourceMapping"`
	Recovered bool            `json:"recovered"`
	SecretBindResult
}

type envImportPlan struct {
	EnvImportPlan
	profileID string
	cfg       configuration
	resource  ResourceMapping
}

func planEnvImport(ctx context.Context, q taskReader, loc configurationLocation, path string, in EnvImport) (envImportPlan, error) {
	var out envImportPlan
	p, cfg, err := secretProfile(ctx, q, loc, in.Profile)
	if err != nil {
		return out, err
	}
	task, err := loadTask(ctx, q, loc.project.ID, in.Task)
	if err != nil {
		return out, err
	}
	req, ok := task.requirement(in.Requirement)
	if !ok {
		return out, Fail("incompatible", "Task has no credential requirement with that name")
	}
	resource, err := loadMappedResource(ctx, q, in.ResourceID)
	if err != nil {
		return out, err
	}
	if err := requireNoBindingConflict(ctx, q, task.ID, resource.ID, resource.Scope.ID); err != nil {
		return out, err
	}
	if task.Policy.Storage != "shared" || resource.Policy.Storage != "shared" || !compatibleResource(task.TaskSpec, req.ResourceSlot, resource.ResourceSpec) {
		return out, Fail("incompatible", "Task and Resource must be shared and the Resource must match the slot kind and stage")
	}
	mapping := ResourceMapping{TaskID: task.ID, Slot: req.ResourceSlot, ResourceID: resource.ID}
	i := slices.IndexFunc(cfg.ResourceMappings, func(m ResourceMapping) bool { return m.TaskID == task.ID && m.Slot == req.ResourceSlot })
	if i < 0 {
		cfg.ResourceMappings = append(cfg.ResourceMappings, mapping)
	} else if cfg.ResourceMappings[i] != mapping {
		return out, Fail("resource-mapping-exists", "this Profile already maps the Task slot to another Resource")
	}
	if slices.ContainsFunc(cfg.SecretMappings, func(m SecretMapping) bool {
		return m.TaskID == task.ID && m.Requirement == req.Name
	}) {
		return out, Fail("secret-mapping-exists", "this requirement already has a SecretBinding; inspect it with secret status")
	}
	if len(cfg.ResourceMappings) > 512 || len(cfg.SecretMappings) >= 512 {
		return out, Fail("size-limit", "Profile has no capacity for another mapping")
	}
	spec := SecretBindingSpec{in.Alias, resource.ID, req.Purpose, req.Type, in.ProviderProfile, in.Locator}
	if err := validateSecretBinding(spec); err != nil {
		return out, err
	}
	plan := EnvImportPlan{ProjectID: loc.project.ID, Profile: p.Name, TaskID: task.ID, Task: task.Name,
		Field: in.Field, Requirement: req, ResourcePurpose: resource.Purpose, Binding: spec, Recover: in.Recover}
	// Bind confirmation to the selected file path, workspace and catalog
	// revisions. The digest never includes source values.
	wire, err := json.Marshal(struct {
		Plan      EnvImportPlan
		Revisions []string
	}{plan, []string{path, loc.workspace, loc.device, p.ID, cfg.Revision, task.Revision, resource.Revision}})
	if err != nil {
		return out, err
	}
	plan.Preview = digest(wire)
	return envImportPlan{EnvImportPlan: plan, profileID: p.ID, cfg: cfg, resource: mapping}, nil
}

func (a App) ImportOnboardingEnv(ctx context.Context, selector, path string, in EnvImport) (any, error) {
	if selector == "" || in.Profile == "" || in.Task == "" || in.Requirement == "" || in.Field == "" || !validUUID(in.ResourceID) {
		return nil, Fail("usage", "choose a Project, Profile, Task, requirement, source field and Resource UUID")
	}
	if in.Locator.EntryID != "" || !validLocator(in.Locator) || !validProviderProfile(in.ProviderProfile) {
		return nil, Fail("usage", "choose a provider, provider profile, method and exact backend Project, environment, path and key")
	}
	adapter, methods, err := a.providerAdapter(in.ProviderProfile)
	if err != nil {
		return nil, err
	}
	importer, ok := adapter.(provider.Importer)
	if !ok || !slices.ContainsFunc(methods, func(m provider.MethodCapabilities) bool {
		return m.Method == in.ProviderProfile.Method && m.CreateImport
	}) {
		return nil, Fail("import-unsupported", "the selected provider method does not support create-only import")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, Fail("invalid-env-file", "could not resolve the selected env file path")
	}
	file, err := readEnvFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(file.body)
	value, ok := file.values[in.Field]
	if !ok {
		return nil, Fail("env-field-missing", "the selected source field is absent from the env file")
	}
	if in.Confirm != "" {
		release, err := a.syncOwner(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	resolution, err := resolveProjectAt(ctx, db, selector, Resolution{}, false)
	if err != nil {
		return nil, err
	}
	if err := requireSubscription(ctx, db, resolution.Project.ID); err != nil {
		return nil, err
	}
	loc := configurationLocation{project: *resolution.Project, device: resolution.Device.ID}
	if err := db.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&loc.workspace); err != nil {
		return nil, err
	}
	plan, err := planEnvImport(ctx, db, loc, path, in)
	if err != nil {
		return nil, err
	}
	if in.Confirm == "" {
		return plan.EnvImportPlan, nil
	}
	if in.Confirm != plan.Preview {
		return nil, Fail("env-preview-changed", "preview this exact file path and target mapping again before confirming")
	}
	if err := a.prepareBaseline(ctx, db); err != nil {
		return nil, err
	}
	var pushPending bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sync_state WHERE pending <> '')").Scan(&pushPending); err != nil {
		return nil, err
	}
	if pushPending {
		return nil, Fail("push-pending", "reconcile the pending push before editing shared catalog")
	}

	if err := a.transferEnvValue(ctx, loc, adapter, importer, in, value); err != nil {
		return nil, err
	}

	receipt := EnvImportReceipt{ProjectID: loc.project.ID, Field: in.Field, Resource: plan.resource, Recovered: in.Recover}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		current, err := planEnvImport(ctx, c, loc, path, in)
		if err != nil {
			return err
		}
		if current.Preview != plan.Preview {
			return Fail("env-preview-changed", "catalog mapping changed during import")
		}
		binding, err := insertSecretBinding(ctx, c, loc, current.Binding)
		if err != nil {
			return err
		}
		mapping := SecretMapping{TaskID: current.TaskID, Requirement: current.Requirement.Name, BindingID: binding.ID}
		current.cfg.SecretMappings = append(current.cfg.SecretMappings, mapping)
		if err := saveSecretMappings(ctx, c, current.profileID, binding.Scope, current.cfg); err != nil {
			return err
		}
		receipt.SecretBindResult = SecretBindResult{Profile: current.Profile, Binding: &binding, Mapping: mapping}
		return nil
	})
	if err != nil {
		return nil, Fail("env-catalog-commit-failed", "provider entry is present but catalog commit could not be confirmed; check secret status and use a fresh --recover preview if the mapping is missing")
	}
	return receipt, nil
}

func (a App) transferEnvValue(ctx context.Context, loc configurationLocation, adapter provider.Adapter, importer provider.Importer, in EnvImport, value []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	key := provider.SessionKey{WorkspaceID: loc.workspace, DeviceID: loc.device, Profile: in.ProviderProfile}
	session, err := a.loadProviderSession(ctx, key)
	defer clear(session)
	if err != nil || len(session) == 0 {
		return Fail("authentication-required", "sign in with the selected device provider profile")
	}
	if in.Recover {
		existing, err := adapter.Resolve(ctx, in.ProviderProfile, session, in.Locator)
		defer clear(existing)
		switch {
		case errors.Is(err, provider.ErrAuthenticationRequired):
			return Fail("authentication-required", "sign in with the selected device provider profile")
		case err != nil:
			return Fail("env-recovery-unavailable", "could not read the exact provider entry; check its availability and read permission")
		case !bytes.Equal(value, existing):
			return Fail("env-recovery-mismatch", "provider entry differs from the selected source field")
		}
		return nil
	}
	err = importer.Import(ctx, in.ProviderProfile, session, in.Locator, value)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, provider.ErrAuthenticationRequired):
		return Fail("authentication-required", "sign in with the selected device provider profile")
	case errors.Is(err, provider.ErrImportCollision):
		return Fail("env-provider-collision", "provider entry already exists; choose another key or explicitly preview --recover to verify it against the source field")
	case errors.Is(err, provider.ErrImportPending):
		return Fail("env-provider-approval-pending", "provider approval is pending; after approval use --recover to verify and map the entry")
	case errors.Is(err, provider.ErrImportRejected):
		return Fail("env-provider-rejected", "provider rejected creation; check permissions, target and the provider's literal value restrictions")
	default:
		return Fail("env-provider-outcome-unknown", "provider creation could not be confirmed; verify the target with --recover before retrying creation")
	}
}
