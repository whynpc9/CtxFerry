package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

// trustFormat versions the reviewed field set. Adding a reviewed field changes
// every digest, so devices review again rather than trusting unseen fields.
const trustFormat = "ctxferry-execution-trust-v1"

// ReviewedDefinition is the part of an execution definition a device reviews
// and enables. Descriptive text (Task purpose and inputs, names, aliases) and
// record revisions are excluded, so editing them keeps the enabled state.
type ReviewedDefinition struct {
	Format             string               `json:"format"`
	TaskID             string               `json:"taskId"`
	MembershipID       string               `json:"membershipId"`
	RepositoryID       string               `json:"repositoryId"`
	CheckoutRoot       string               `json:"checkoutRoot"`
	MembershipRoot     string               `json:"membershipRoot"`
	WorkingDirectory   string               `json:"workingDirectory"`
	Program            string               `json:"program"`
	Argv               []string             `json:"argv"`
	AllowCustomCommand bool                 `json:"allowCustomCommand"`
	ProfileID          string               `json:"profileId"`
	Credentials        []ReviewedCredential `json:"credentials"`
}

// ReviewedCredential is one reviewed delivery: the requirement, the Profile's
// Resource and SecretBinding mapping, the binding's provider profile and
// locator, and the Resource purpose. A standalone credential has no Resource;
// its Resource fields are omitted, so slot-attached credentials keep the
// encoding, and the digest, they had before standalone credentials existed.
type ReviewedCredential struct {
	Requirement     string           `json:"requirement"`
	Env             string           `json:"env"`
	Purpose         string           `json:"purpose"`
	Type            string           `json:"type"`
	ResourceSlot    string           `json:"resourceSlot,omitempty"`
	ResourceID      string           `json:"resourceId,omitempty"`
	ResourcePurpose string           `json:"resourcePurpose,omitempty"`
	BindingID       string           `json:"bindingId"`
	ProviderProfile provider.Profile `json:"providerProfile"`
	Locator         provider.Locator `json:"locator"`
}

// Reviewed returns the fields that the device trust decision covers.
func (d ExecutionDefinition) Reviewed() ReviewedDefinition {
	r := ReviewedDefinition{Format: trustFormat, TaskID: d.TaskID, MembershipID: d.MembershipID, RepositoryID: d.RepositoryID,
		CheckoutRoot: d.CheckoutRoot, MembershipRoot: d.MembershipRoot, WorkingDirectory: d.WorkingDirectory, Program: d.Program,
		Argv: slices.Clone(d.Argv), AllowCustomCommand: d.AllowCustomCommand, ProfileID: d.ProfileID,
		Credentials: make([]ReviewedCredential, 0, len(d.Credentials))}
	if r.Argv == nil {
		r.Argv = []string{}
	}
	for _, c := range d.Credentials {
		r.Credentials = append(r.Credentials, ReviewedCredential{Requirement: c.Requirement, Env: c.Env, Purpose: c.Purpose,
			Type: c.Type, ResourceSlot: c.ResourceSlot, ResourceID: c.ResourceID, ResourcePurpose: c.ResourcePurpose,
			BindingID: c.BindingID, ProviderProfile: c.ProviderProfile, Locator: c.Locator})
	}
	slices.SortFunc(r.Credentials, func(a, b ReviewedCredential) int { return strings.Compare(a.Requirement, b.Requirement) })
	return r
}

// Digest identifies a reviewed definition. It is also the preview token that
// enabling must present, so a definition that changed after the preview is
// never enabled by mistake.
func (r ReviewedDefinition) Digest() string {
	raw, err := json.Marshal(r)
	if err != nil {
		panic(err) // Plain strings and slices always encode.
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ExecutionChange is one reviewed field whose current value differs from the
// enabled one. Values are JSON and never contain secret values.
type ExecutionChange struct {
	Field   string          `json:"field"`
	Enabled json.RawMessage `json:"enabled,omitempty"`
	Current json.RawMessage `json:"current,omitempty"`
}

// ExecutionReview is the configuration-change preview for one Task, Profile
// and checkout on this device.
type ExecutionReview struct {
	TaskID  string `json:"taskId"`
	Task    string `json:"task"`
	Profile string `json:"profile,omitempty"`
	// State is enabled, not-enabled, execution-definition-changed or
	// not-required (the Task receives no credentials).
	State      string              `json:"state"`
	Preview    string              `json:"preview,omitempty"`
	Definition *ReviewedDefinition `json:"definition,omitempty"`
	Enabled    *ReviewedDefinition `json:"enabled,omitempty"`
	EnabledAt  string              `json:"enabledAt,omitempty"`
	Changes    []ExecutionChange   `json:"changes"`
	// CustomCommands states that exec may run any caller-chosen program with
	// these credentials once this definition is enabled.
	CustomCommands bool `json:"customCommands"`
}

type trustRecord struct {
	digest, enabledAt string
	definition        ReviewedDefinition
}

// LocalTrust is the device-local ExecutionTrust backed by the local store.
// Its records are never exported to the sync repository.
func LocalTrust(dataDir string) ExecutionTrust { return localTrust{dataDir} }

type localTrust struct{ dataDir string }

func (t localTrust) Enabled(ctx context.Context, d ExecutionDefinition) error {
	if d.CustomCommand != nil && !d.AllowCustomCommand {
		return Fail("custom-command-not-allowed", "Task does not allow custom commands")
	}
	db, err := store.Open(ctx, t.dataDir, false)
	if err != nil {
		return err
	}
	defer db.Close()
	reviewed := d.Reviewed()
	record, err := loadTrust(ctx, db, reviewed)
	if err != nil {
		return err
	}
	if record != nil && record.digest == reviewed.Digest() {
		return nil
	}
	return trustFailure(d, reviewed, record)
}

func trustFailure(d ExecutionDefinition, current ReviewedDefinition, record *trustRecord) error {
	details := map[string]any{"taskId": d.TaskID, "profile": d.Profile, "checkoutRoot": d.CheckoutRoot,
		"membershipRoot": d.MembershipRoot, "state": "not-enabled"}
	message := "credentialed execution of this Task definition is not enabled on this device; review it with ctxferry task trust preview"
	if record != nil {
		details["state"] = "execution-definition-changed"
		fields := []string{}
		for _, change := range executionChanges(record.definition, current) {
			fields = append(fields, change.Field)
		}
		details["changedFields"] = fields
		message = "the Task's execution definition changed since it was enabled on this device; review it with ctxferry task trust preview"
	}
	return &Error{Code: "execution-definition-changed", Message: message, Details: details}
}

// loadTrust returns this device's approval for exactly this Task, Profile,
// checkout and membership root. When there is none, it returns the latest
// approval of the same Task and Profile that this definition replaces: one in
// the same checkout whose membership root moved (an in-place subdirectory
// change or a move to another membership of the Repository), or one for
// another Repository. Another checkout of the same Repository is a separate
// approval, not a change.
// Authorization still requires the exact record.
func loadTrust(ctx context.Context, q rowQuerier, r ReviewedDefinition) (*trustRecord, error) {
	record, err := scanTrust(q.QueryRowContext(ctx, `SELECT t.digest,t.definition,t.enabled_at FROM execution_trust t JOIN device d ON d.id=t.device_id
		WHERE t.task_id=? AND t.profile_id=? AND t.checkout_root=? AND t.membership_root=?`, r.TaskID, r.ProfileID, r.CheckoutRoot, r.MembershipRoot))
	if record != nil || err != nil {
		return record, err
	}
	return scanTrust(q.QueryRowContext(ctx, `SELECT t.digest,t.definition,t.enabled_at FROM execution_trust t JOIN device d ON d.id=t.device_id
		WHERE t.task_id=? AND t.profile_id=? AND (t.checkout_root=? OR coalesce(json_extract(t.definition,'$.repositoryId'),'')<>?)
		ORDER BY t.enabled_at DESC, t.checkout_root, t.membership_root LIMIT 1`, r.TaskID, r.ProfileID, r.CheckoutRoot, r.RepositoryID))
}

// trustStamp uses a fixed-width UTC timestamp so that approvals order
// correctly as text, including several within one second.
func trustStamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func scanTrust(row *sql.Row) (*trustRecord, error) {
	var out trustRecord
	var raw string
	err := row.Scan(&out.digest, &raw, &out.enabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out.definition); err != nil {
		// An unreadable record is treated as a changed definition.
		out.definition = ReviewedDefinition{}
	}
	return &out, nil
}

// executionChanges lists reviewed fields that differ, keyed by requirement for
// credential deliveries.
func executionChanges(enabled, current ReviewedDefinition) []ExecutionChange {
	flat := func(r ReviewedDefinition) map[string]any {
		m := map[string]any{"format": r.Format, "taskId": r.TaskID, "membershipId": r.MembershipID, "repositoryId": r.RepositoryID,
			"checkoutRoot": r.CheckoutRoot, "membershipRoot": r.MembershipRoot, "workingDirectory": r.WorkingDirectory, "program": r.Program, "argv": r.Argv,
			"allowCustomCommand": r.AllowCustomCommand, "profileId": r.ProfileID}
		for _, c := range r.Credentials {
			prefix := "credentials." + c.Requirement + "."
			for k, v := range map[string]any{"env": c.Env, "purpose": c.Purpose, "type": c.Type, "resourceSlot": c.ResourceSlot,
				"resourceId": c.ResourceID, "resourcePurpose": c.ResourcePurpose, "bindingId": c.BindingID,
				"providerProfile": c.ProviderProfile, "locator": c.Locator} {
				m[prefix+k] = v
			}
		}
		return m
	}
	a, b := flat(enabled), flat(current)
	keys := []string{}
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out := []ExecutionChange{}
	for _, k := range keys {
		var before, after json.RawMessage
		if v, ok := a[k]; ok {
			before, _ = json.Marshal(v)
		}
		if v, ok := b[k]; ok {
			after, _ = json.Marshal(v)
		}
		if string(before) != string(after) {
			out = append(out, ExecutionChange{Field: k, Enabled: before, Current: after})
		}
	}
	return out
}

// PreviewExecution shows the execution definition that run would use for this
// Task, Profile and checkout, compared with what this device enabled. Its
// preview token is required to enable exactly this definition.
func (a App) PreviewExecution(ctx context.Context, project, cwd, task, checkout, profile string) (ExecutionReview, error) {
	plan, err := a.PrepareTask(ctx, project, cwd, task, checkout, profile)
	if err != nil {
		return ExecutionReview{}, err
	}
	review := ExecutionReview{TaskID: plan.Task.ID, Task: plan.Task.Name, Profile: plan.Profile, Changes: []ExecutionChange{}}
	if len(plan.credentials) == 0 {
		review.State = "not-required"
		return review, nil
	}
	current := plan.Definition().Reviewed()
	review.Definition, review.Preview, review.CustomCommands = &current, current.Digest(), current.AllowCustomCommand
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return review, err
	}
	defer db.Close()
	record, err := loadTrust(ctx, db, current)
	if err != nil {
		return review, err
	}
	switch {
	case record == nil:
		review.State = "not-enabled"
	case record.digest == review.Preview:
		review.State, review.EnabledAt = "enabled", record.enabledAt
	default:
		review.State, review.EnabledAt = "execution-definition-changed", record.enabledAt
		review.Enabled = &record.definition
		review.Changes = executionChanges(record.definition, current)
	}
	return review, nil
}

// Test seams: enableDiscovered runs after discovery and before the write lock
// is taken, enableLocked once it is held.
var enableDiscovered, enableLocked = func() {}, func() {}

// EnableExecution enables the previewed definition on this device. Git and
// filesystem discovery run first; the definition is then prepared again from
// the catalog with that checkout pinned while this device's store write lock is
// held, so no local or synchronized catalog change can commit between the
// check and the stored approval, and no Git command runs under the lock. It is
// refused with preview-stale when the definition no longer matches the
// preview; the stored record is exactly the previewed one.
func (a App) EnableExecution(ctx context.Context, project, cwd, task, checkout, profile, preview string) (ExecutionReview, error) {
	if preview == "" {
		return ExecutionReview{}, Fail("usage", "enable requires --preview from ctxferry task trust preview")
	}
	discovered, err := a.PrepareTask(ctx, project, cwd, task, checkout, profile)
	if err != nil {
		return ExecutionReview{}, err
	}
	stale := func(plan TaskExecution) error {
		return &Error{Code: "preview-stale", Message: "the execution definition differs from the preview; preview it again",
			Details: map[string]string{"taskId": plan.Task.ID, "task": plan.Task.Name}}
	}
	if len(discovered.credentials) == 0 {
		return ExecutionReview{}, Fail("trust-not-required", "Task receives no credentials; it runs without enabling")
	}
	if discovered.Definition().Reviewed().Digest() != preview {
		return ExecutionReview{}, stale(discovered)
	}
	enableDiscovered()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return ExecutionReview{}, err
	}
	defer db.Close()
	var out ExecutionReview
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		enableLocked()
		pin := discovered.checkout
		plan, err := a.prepareTask(ctx, discovered.Task.Scope.ID, cwd, discovered.Task.ID, "", profile, nil, &pin)
		if err != nil {
			return err
		}
		if len(plan.credentials) == 0 {
			return Fail("trust-not-required", "Task receives no credentials; it runs without enabling")
		}
		current := plan.Definition().Reviewed()
		if current.Digest() != preview {
			return stale(plan)
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		stamp := trustStamp(time.Now())
		if _, err := c.ExecContext(ctx, `INSERT INTO execution_trust SELECT id,?,?,?,?,?,?,? FROM device WHERE singleton=1
			ON CONFLICT(device_id,task_id,profile_id,checkout_root,membership_root) DO UPDATE SET digest=excluded.digest,definition=excluded.definition,enabled_at=excluded.enabled_at`,
			current.TaskID, current.ProfileID, current.CheckoutRoot, current.MembershipRoot, preview, string(raw), stamp); err != nil {
			return err
		}
		out = ExecutionReview{TaskID: plan.Task.ID, Task: plan.Task.Name, Profile: plan.Profile, State: "enabled", Preview: preview,
			Definition: &current, EnabledAt: stamp, Changes: []ExecutionChange{}, CustomCommands: current.AllowCustomCommand}
		return nil
	})
	return out, err
}
