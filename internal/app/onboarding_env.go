package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
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

// EnvImport selects one or more fields of an env file to create as provider
// entries and map, each to the Task requirement it delivers.
type EnvImport struct {
	SecretTarget
	// Fields are source variable names. With several fields each one maps to
	// the requirement whose delivery variable has the same name.
	Fields     []string
	ResourceID string
	// Like, Alias, ProviderProfile and Locator follow secret bind: given
	// values win, then the Like template, then the requirement's defaults.
	Like            string
	Alias           string
	ProviderProfile provider.Profile
	Locator         provider.Locator
	// Recover names fields whose existing provider entry is verified against
	// the source instead of created; "*" names every field.
	Recover []string
	Confirm string
}

// RecoverAll as the only --recover value selects every imported field.
const RecoverAll = "*"

type EnvImportItem struct {
	Field           string                `json:"field"`
	Requirement     CredentialRequirement `json:"requirement"`
	ResourcePurpose string                `json:"resourcePurpose,omitempty"`
	Binding         SecretBindingSpec     `json:"binding"`
	Recover         bool                  `json:"recover"`
}

type EnvImportPlan struct {
	Preview   string `json:"preview"`
	ProjectID string `json:"projectId"`
	Profile   string `json:"profile"`
	// CreateProfile reports that confirming creates the default Profile.
	CreateProfile bool            `json:"createProfile,omitempty"`
	TaskID        string          `json:"taskId"`
	Task          string          `json:"task"`
	Imports       []EnvImportItem `json:"imports"`
}

type EnvImportResult struct {
	Field     string           `json:"field"`
	Resource  *ResourceMapping `json:"resourceMapping,omitempty"`
	Binding   SecretBinding    `json:"binding"`
	Mapping   SecretMapping    `json:"mapping"`
	Recovered bool             `json:"recovered"`
}

type EnvImportReceipt struct {
	ProjectID string            `json:"projectId"`
	Profile   string            `json:"profile"`
	Imports   []EnvImportResult `json:"imports"`
}

type envImportPlan struct {
	EnvImportPlan
	profileID string
	cfg       configuration
	resources map[string]ResourceMapping // by field
}

func (in EnvImport) recovers(field string) bool {
	return slices.Contains(in.Recover, RecoverAll) || slices.Contains(in.Recover, field)
}

// envImportProfile resolves the target Profile like secret bind does: the
// explicit or selected Profile, then default, else a default Profile that the
// confirmation creates when the Project has none.
func envImportProfile(ctx context.Context, q taskReader, loc configurationLocation, name string) (*SelectedProfile, configuration, bool, error) {
	if name == "" {
		p, cfg, err := pendingDefaultProfile(ctx, q, loc)
		if err != nil || p != nil {
			return p, cfg, p != nil, err
		}
	}
	p, cfg, err := secretProfile(ctx, q, loc, name)
	return p, cfg, false, err
}

func planEnvImport(ctx context.Context, q taskReader, loc configurationLocation, path string, in EnvImport) (envImportPlan, error) {
	var out envImportPlan
	p, cfg, create, err := envImportProfile(ctx, q, loc, in.Profile)
	if err != nil {
		return out, err
	}
	task, err := loadTask(ctx, q, loc.project.ID, in.Task)
	if err != nil {
		return out, err
	}
	if err := requireNoBindingConflict(ctx, q, task.ID); err != nil {
		return out, err
	}
	if task.Policy.Storage != "shared" {
		return out, Fail("incompatible", "a credential mapping requires a shared Task")
	}
	revisions := []string{path, loc.workspace, loc.device, p.ID, cfg.Revision, task.Revision}
	if create {
		// A Profile that does not exist yet has no revision.
		revisions[4] = "create-default"
	}
	var resource *Resource
	if in.ResourceID != "" {
		r, err := loadMappedResource(ctx, q, in.ResourceID)
		if err != nil {
			return out, err
		}
		if err := requireNoBindingConflict(ctx, q, r.ID, r.Scope.ID); err != nil {
			return out, err
		}
		resource = &r
		revisions = append(revisions, r.Revision)
	}
	plan := EnvImportPlan{ProjectID: loc.project.ID, Profile: p.Name, CreateProfile: create, TaskID: task.ID, Task: task.Name}
	resources := map[string]ResourceMapping{}
	usedResource := false
	for _, field := range in.Fields {
		req, err := envRequirement(task, in, field)
		if err != nil {
			return out, err
		}
		if slices.ContainsFunc(cfg.SecretMappings, func(m SecretMapping) bool {
			return m.TaskID == task.ID && m.Requirement == req.Name
		}) {
			return out, Fail("secret-mapping-exists", "requirement "+req.Name+" already has a SecretBinding; inspect it with secret status")
		}
		item := EnvImportItem{Field: field, Requirement: req, Recover: in.recovers(field)}
		var resourceID string
		if !req.standalone() {
			mapping, mapped, err := envResourceMapping(ctx, q, &cfg, task, req, resource)
			if err != nil {
				return out, err
			}
			usedResource = usedResource || resource != nil
			if resource == nil {
				revisions = append(revisions, mapped.Revision)
			}
			resources[field], resourceID, item.ResourcePurpose = mapping, mapping.ResourceID, mapped.Purpose
		}
		change := SecretBindingChange{Like: in.Like, Alias: in.Alias, ProviderProfile: in.ProviderProfile, Locator: in.Locator}
		item.Binding, err = newBindingSpec(ctx, q, loc.project, req, resourceID, change)
		if err != nil {
			return out, err
		}
		if item.Binding.Locator.EntryID != "" {
			return out, Fail("usage", "env import creates named entries; choose a backend project, environment, path and key")
		}
		plan.Imports = append(plan.Imports, item)
	}
	if resource != nil && !usedResource {
		return out, Fail("usage", "--resource applies only to requirements attached to a Resource slot")
	}
	if len(cfg.ResourceMappings) > 512 || len(cfg.SecretMappings)+len(plan.Imports) > 512 {
		return out, Fail("size-limit", "Profile has no capacity for another mapping")
	}
	for _, item := range plan.Imports[1:] {
		if item.Binding.ProviderProfile != plan.Imports[0].Binding.ProviderProfile {
			return out, Fail("usage", "one import uses one provider profile")
		}
	}
	// Bind confirmation to the selected file path, workspace and catalog
	// revisions. The digest never includes source values.
	wire, err := json.Marshal(struct {
		Plan      EnvImportPlan
		Revisions []string
	}{plan, revisions})
	if err != nil {
		return out, err
	}
	plan.Preview = digest(wire)
	return envImportPlan{EnvImportPlan: plan, profileID: p.ID, cfg: cfg, resources: resources}, nil
}

// envRequirement selects the requirement a field imports into: the named one
// for a single field, else the one whose delivery variable is the field.
func envRequirement(task Task, in EnvImport, field string) (CredentialRequirement, error) {
	if in.Requirement != "" {
		req, ok := task.requirement(in.Requirement)
		if !ok {
			return req, Fail("incompatible", "Task has no credential requirement with that name")
		}
		return req, nil
	}
	i := slices.IndexFunc(task.CredentialRequirements, func(r CredentialRequirement) bool { return r.Env == field })
	if i < 0 {
		return CredentialRequirement{}, Fail("incompatible", "Task has no credential requirement delivered as "+field+"; pass --requirement for a single field")
	}
	return task.CredentialRequirements[i], nil
}

// envResourceMapping returns the Resource mapping a slot-attached requirement
// imports with: the selected Resource, added to cfg when the slot is not yet
// mapped, or the Profile's existing mapping when none is selected.
func envResourceMapping(ctx context.Context, q taskReader, cfg *configuration, task Task, req CredentialRequirement, selected *Resource) (ResourceMapping, Resource, error) {
	if selected == nil {
		resource, err := secretResource(ctx, q, *cfg, task, req)
		if Classify(err).Code == "binding-missing" {
			return ResourceMapping{}, resource, Fail("usage", "choose the Resource for slot "+req.ResourceSlot+" with --resource")
		}
		return ResourceMapping{TaskID: task.ID, Slot: req.ResourceSlot, ResourceID: resource.ID}, resource, err
	}
	if selected.Policy.Storage != "shared" || !compatibleResource(task.TaskSpec, req.ResourceSlot, selected.ResourceSpec) {
		return ResourceMapping{}, *selected, Fail("incompatible", "Task and Resource must be shared and the Resource must match the slot kind and stage")
	}
	mapping := ResourceMapping{TaskID: task.ID, Slot: req.ResourceSlot, ResourceID: selected.ID}
	i := slices.IndexFunc(cfg.ResourceMappings, func(m ResourceMapping) bool { return m.TaskID == task.ID && m.Slot == req.ResourceSlot })
	if i < 0 {
		cfg.ResourceMappings = append(cfg.ResourceMappings, mapping)
	} else if cfg.ResourceMappings[i] != mapping {
		return mapping, *selected, Fail("resource-mapping-exists", "this Profile already maps the Task slot to another Resource")
	}
	return mapping, *selected, nil
}

func (in EnvImport) validate() error {
	if in.Task == "" || len(in.Fields) == 0 || len(in.Fields) > 32 {
		return Fail("usage", "choose a Project, Task and between 1 and 32 source fields")
	}
	if len(in.Fields) > 1 && (in.Requirement != "" || in.Alias != "" || in.Locator.Key != "") {
		return Fail("usage", "--requirement, --alias and --key apply to a single field; several fields map by variable name")
	}
	if in.ResourceID != "" && !validUUID(in.ResourceID) {
		return Fail("usage", "--resource takes a shared Resource UUID")
	}
	if in.Locator.EntryID != "" {
		return Fail("usage", "env import creates named entries; choose a backend project, environment, path and key")
	}
	for i, field := range in.Fields {
		if !environmentName.MatchString(field) || slices.Contains(in.Fields[:i], field) {
			return Fail("usage", "source fields must be distinct variable names")
		}
	}
	for _, field := range in.Recover {
		if field != RecoverAll && !slices.Contains(in.Fields, field) {
			return Fail("usage", "--recover names a field that is not imported")
		}
	}
	return nil
}

func (a App) ImportOnboardingEnv(ctx context.Context, selector, path string, in EnvImport) (any, error) {
	if selector == "" {
		return nil, Fail("usage", "choose a Project with --project")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, Fail("invalid-env-file", "could not resolve the selected env file path")
	}
	file, err := readEnvFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(file.body)
	for _, field := range in.Fields {
		if _, ok := file.values[field]; !ok {
			return nil, Fail("env-field-missing", "the selected source field "+field+" is absent from the env file")
		}
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
	profile := plan.Imports[0].Binding.ProviderProfile
	adapter, methods, err := a.providerAdapter(profile)
	if err != nil {
		return nil, err
	}
	importer, ok := adapter.(provider.Importer)
	if !ok || !slices.ContainsFunc(methods, func(m provider.MethodCapabilities) bool {
		return m.Method == profile.Method && m.CreateImport
	}) {
		return nil, Fail("import-unsupported", "the selected provider method does not support create-only import")
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

	var created []string
	for _, item := range plan.Imports {
		err := a.transferEnvValue(ctx, loc, adapter, importer, profile, item.Binding.Locator, item.Recover, file.values[item.Field])
		if err != nil {
			return nil, partialEnvImport(err, item.Field, created)
		}
		if !item.Recover {
			created = append(created, item.Field)
		}
	}

	receipt := EnvImportReceipt{ProjectID: loc.project.ID, Profile: plan.Profile}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		current, err := planEnvImport(ctx, c, loc, path, in)
		if err != nil {
			return err
		}
		if current.Preview != plan.Preview {
			return Fail("env-preview-changed", "catalog mapping changed during import")
		}
		scope := Scope{"project", loc.project.ID}
		for _, item := range current.Imports {
			binding, err := insertSecretBinding(ctx, c, loc, item.Binding)
			if err != nil {
				return err
			}
			mapping := SecretMapping{TaskID: current.TaskID, Requirement: item.Requirement.Name, BindingID: binding.ID}
			current.cfg.SecretMappings = append(current.cfg.SecretMappings, mapping)
			result := EnvImportResult{Field: item.Field, Binding: binding, Mapping: mapping, Recovered: item.Recover}
			if m, ok := current.resources[item.Field]; ok {
				result.Resource = &m
			}
			receipt.Imports = append(receipt.Imports, result)
		}
		return saveSecretMappings(ctx, c, current.profileID, scope, current.cfg)
	})
	if err != nil {
		return nil, Fail("env-catalog-commit-failed", "provider entries are present but catalog commit could not be confirmed; check secret status and use a fresh --recover preview for any mapping that is missing")
	}
	return receipt, nil
}

// partialEnvImport names the field that failed and the fields whose provider
// entries an earlier step of the same confirmation already created, so they
// are mapped with --recover instead of created again.
func partialEnvImport(err error, field string, created []string) error {
	failure := *Classify(err)
	details := map[string]any{"field": field}
	if len(created) > 0 {
		details["created"] = created
		failure.Message += "; provider entries were already created for " + strings.Join(created, ", ") +
			": preview again with --recover=<field> for each"
	}
	failure.Details = details
	return &failure
}

func (a App) transferEnvValue(ctx context.Context, loc configurationLocation, adapter provider.Adapter, importer provider.Importer,
	profile provider.Profile, locator provider.Locator, recover bool, value []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	key := provider.SessionKey{WorkspaceID: loc.workspace, DeviceID: loc.device, Profile: profile}
	session, err := a.loadProviderSession(ctx, key)
	defer clear(session)
	if err != nil || len(session) == 0 {
		return Fail("authentication-required", "sign in with the selected device provider profile")
	}
	if recover {
		existing, err := adapter.Resolve(ctx, profile, session, locator)
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
	err = importer.Import(ctx, profile, session, locator, value)
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
