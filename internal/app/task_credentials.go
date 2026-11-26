package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

// ExecutionCredential describes one credential delivery of a reviewed
// execution definition. It holds references only, never values. The Resource
// fields are empty for a standalone credential.
type ExecutionCredential struct {
	Requirement     string           `json:"requirement"`
	Env             string           `json:"env"`
	Purpose         string           `json:"purpose"`
	Type            string           `json:"type"`
	ResourceSlot    string           `json:"resourceSlot"`
	ResourceID      string           `json:"resourceId"`
	ResourcePurpose string           `json:"resourcePurpose"`
	BindingID       string           `json:"bindingId"`
	BindingRevision string           `json:"bindingRevision"`
	ProviderProfile provider.Profile `json:"providerProfile"`
	Locator         provider.Locator `json:"locator"`
}

// ExecutionDefinition is what a credentialed run would execute on this device:
// the Task's execution fields, the membership root in the resolved local
// checkout, the working directory, and the Profile's credential mapping chain.
type ExecutionDefinition struct {
	TaskID       string `json:"taskId"`
	TaskRevision string `json:"taskRevision"`
	MembershipID string `json:"membershipId"`
	RepositoryID string `json:"repositoryId"`
	// CheckoutRoot is the selected local Git checkout or worktree root.
	CheckoutRoot string `json:"checkoutRoot"`
	// Program and Argv are the Task's registered command.
	Program            string                `json:"program"`
	Argv               []string              `json:"argv"`
	AllowCustomCommand bool                  `json:"allowCustomCommand"`
	MembershipRoot     string                `json:"membershipRoot"`
	WorkingDirectory   string                `json:"workingDirectory"`
	ProfileID          string                `json:"profileId"`
	Profile            string                `json:"profile"`
	Credentials        []ExecutionCredential `json:"credentials"`
	// CustomCommand is the caller-chosen command exec launches instead of
	// Program and Argv. It is nil for run. A trust decision may allow it only
	// when the enabled definition allows custom commands.
	CustomCommand *ExecutionCommand `json:"customCommand,omitempty"`
}

// ExecutionCommand is a program and its argument array, never a shell string.
type ExecutionCommand struct {
	Program string   `json:"program"`
	Argv    []string `json:"argv"`
}

// ExecutionTrust is the device-local decision that a credentialed execution
// definition was reviewed and enabled here. It runs before any provider call.
// Enabled returns nil only when exactly this definition may receive
// credentials. Without an implementation credentialed runs fail closed.
type ExecutionTrust interface {
	Enabled(context.Context, ExecutionDefinition) error
}

type plannedCredential struct {
	ExecutionCredential
	resource Resource
}

func credentialFailure(err error, task Task, req CredentialRequirement, profile string) error {
	var failure *Error
	if !errors.As(err, &failure) || failure.Details != nil {
		return err
	}
	out := *failure
	out.Message += " for Task " + task.Name + " requirement " + req.Name
	out.Details = map[string]string{"taskId": task.ID, "task": task.Name, "requirement": req.Name,
		"resourceSlot": req.ResourceSlot, "profile": profile}
	return &out
}

// planCredentials follows only the Task's own requirements through the
// Profile's explicit mappings. It never searches for similar bindings.
func planCredentials(ctx context.Context, q rowQuerier, task Task, profile *SelectedProfile, cfg configuration) ([]plannedCredential, error) {
	out := make([]plannedCredential, 0, len(task.CredentialRequirements))
	if err := requireNoBindingConflict(ctx, q, task.ID, profile.ID); err != nil {
		return nil, err
	}
	for _, req := range task.CredentialRequirements {
		c, err := planCredential(ctx, q, task, req, cfg)
		if err != nil {
			return nil, credentialFailure(err, task, req, profile.Name)
		}
		out = append(out, c)
	}
	return out, nil
}

func planCredential(ctx context.Context, q rowQuerier, task Task, req CredentialRequirement, cfg configuration) (plannedCredential, error) {
	resource, err := secretResource(ctx, q, cfg, task, req)
	if err != nil {
		return plannedCredential{}, err
	}
	i := slices.IndexFunc(cfg.SecretMappings, func(m SecretMapping) bool { return m.TaskID == task.ID && m.Requirement == req.Name })
	if i < 0 {
		return plannedCredential{}, Fail("binding-missing", "Profile has no SecretBinding mapping")
	}
	id := cfg.SecretMappings[i].BindingID
	if err := requireNoBindingConflict(ctx, q, id); err != nil {
		return plannedCredential{}, err
	}
	binding, err := loadSecretBinding(ctx, q, id)
	if err != nil {
		return plannedCredential{}, err
	}
	if err := requireNoBindingConflict(ctx, q, binding.Scope.ID); err != nil {
		return plannedCredential{}, err
	}
	if !compatibleSecret(req, resource.ID, binding.SecretBindingSpec) {
		return plannedCredential{}, incompatibleBinding(req)
	}
	return plannedCredential{ExecutionCredential{
		Requirement: req.Name, Env: req.Env, Purpose: req.Purpose, Type: req.Type, ResourceSlot: req.ResourceSlot,
		ResourceID: resource.ID, ResourcePurpose: resource.Purpose, BindingID: binding.ID, BindingRevision: binding.Revision,
		ProviderProfile: binding.ProviderProfile, Locator: binding.Locator,
	}, resource}, nil
}

// credentialEnvironment authorizes, checks reachability and resolves each
// delivered variable. Values exist only in memory for this invocation.
func (a App) credentialEnvironment(ctx context.Context, plan TaskExecution) ([]string, [][]byte, error) {
	if a.Trust == nil {
		return nil, nil, Fail("execution-definition-changed", "credentialed execution of this Task definition is not enabled on this device")
	}
	if err := a.Trust.Enabled(ctx, plan.Definition()); err != nil {
		var failure *Error
		if errors.As(err, &failure) {
			return nil, nil, err
		}
		return nil, nil, Fail("execution-definition-changed", "credentialed execution of this Task definition is not enabled on this device")
	}
	if err := a.requireReachable(ctx, plan); err != nil {
		return nil, nil, err
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return nil, nil, err
	}
	var workspace, device string
	err = db.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&workspace, &device)
	db.Close()
	if err != nil {
		return nil, nil, err
	}
	sessions := map[provider.Profile][]byte{}
	defer func() {
		for _, session := range sessions {
			clear(session)
		}
	}()
	env := make([]string, 0, len(plan.credentials))
	values := make([][]byte, 0, len(plan.credentials))
	fail := func(err error, c plannedCredential) ([]string, [][]byte, error) {
		for _, value := range values {
			clear(value)
		}
		req := CredentialRequirement{Name: c.Requirement, ResourceSlot: c.ResourceSlot}
		return nil, nil, credentialFailure(err, plan.Task, req, plan.Profile)
	}
	for _, c := range plan.credentials {
		adapter, _, err := a.providerAdapter(c.ProviderProfile)
		if err != nil {
			return fail(Fail("secret-unavailable", Classify(err).Message), c)
		}
		session, ok := sessions[c.ProviderProfile]
		if !ok {
			key := provider.SessionKey{WorkspaceID: workspace, DeviceID: device, Profile: c.ProviderProfile}
			session, err = a.deviceSession(ctx, adapter, key)
			if err != nil {
				return fail(err, c)
			}
			sessions[c.ProviderProfile] = session
		}
		value, err := adapter.Resolve(ctx, c.ProviderProfile, session, c.Locator)
		if err != nil {
			clear(value)
			if errors.Is(err, provider.ErrAuthenticationRequired) {
				return fail(Fail("authentication-required", "provider session is no longer usable"), c)
			}
			return fail(Fail("secret-unavailable", "provider could not retrieve the referenced entry"), c)
		}
		values = append(values, value)
		if !deliverableValue(value) {
			return fail(Fail("secret-unavailable", "provider entry cannot be delivered as an environment variable"), c)
		}
		env = append(env, c.Env+"="+string(value))
	}
	return env, values, nil
}

// deviceSession loads this device's own provider session. Sessions for other
// devices or profiles are never consulted and the session never reaches the
// child.
func (a App) deviceSession(ctx context.Context, adapter provider.Adapter, key provider.SessionKey) ([]byte, error) {
	// The shared loader prefers a runtime session provisioned for exactly this
	// workspace, device and provider profile, then secure device storage.
	session, loadErr := a.loadProviderSession(ctx, key)
	auth, err := sessionAuthentication(ctx, adapter, key.Profile, session, loadErr)
	if err == nil && auth.State != provider.Authenticated {
		err = authenticationFailure(auth)
	}
	if err != nil {
		clear(session)
		return nil, err
	}
	return session, nil
}

// requireReachable reports the selected Resources' latest reachability on
// this device's resolved route. It opens no connection: `resource check` is the
// explicit probe. It never substitutes another Resource with the same identity.
func (a App) requireReachable(ctx context.Context, plan TaskExecution) error {
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return err
	}
	defer db.Close()
	seen := map[string]bool{}
	for _, c := range plan.credentials {
		// A standalone credential has no Resource and nothing to reach.
		if c.ResourceID == "" || seen[c.ResourceID] {
			continue
		}
		seen[c.ResourceID] = true
		detail, err := resourceDetail(ctx, db, c.resource.Scope.ID, plan.launch.dir, c.ResourceID)
		if err != nil {
			return err
		}
		facts := map[string]string{"resourceId": c.ResourceID, "identity": detail.Resource.Identity,
			"route": detail.Route.Status, "network": detail.Route.Network}
		switch detail.Route.Status {
		case "resolved":
		case "route-missing":
			// No TCP endpoint is declared for this Resource; nothing to diagnose.
			continue
		case "platform-unsupported":
			return Fail("platform-unsupported", "mapped Resource does not apply to the current platform")
		default:
			return &Error{Code: "resource-unreachable", Message: "Resource " + detail.Resource.Identity +
				" has no usable route from this execution environment (" + detail.Route.Status + ")", Details: facts}
		}
		observation, err := latestServiceObservation(ctx, db, detail)
		if err != nil {
			return err
		}
		if observation != nil && observation.Status == "unreachable" {
			facts["target"], facts["observedAt"] = observation.Scope.Target, observation.ObservedAt
			return &Error{Code: "resource-unreachable", Message: "Resource " + detail.Resource.Identity +
				" was unreachable at " + observation.Scope.Target + " from this device; run resource check after fixing the network", Details: facts}
		}
	}
	return nil
}

// deliverableValue reports whether the child receives exactly these bytes.
// Windows environments are UTF-16, so invalid UTF-8 would be altered there and
// then escape byte-exact redaction.
func deliverableValue(value []byte) bool {
	return bytes.IndexByte(value, 0) < 0 && (runtime.GOOS != "windows" || utf8.Valid(value))
}

const redactionMarker = "[REDACTED]"

// redactor replaces delivered values in a child output stream, choosing the
// longest value at the leftmost position. A KMP state per value tracks the
// earliest position where a value could still begin, so output is held only
// while it may be part of a value and each byte is examined once per value.
// This reduces accidental disclosure; it cannot stop a program that encodes or
// transmits what it was given.
type redactor struct {
	w       io.Writer
	values  [][]byte
	fail    [][]int
	state   []int
	matches map[int]int // absolute start offset -> longest complete match
	pending []byte
	base    int // absolute offset of pending[0]
	err     error
}

func newRedactor(w io.Writer, values [][]byte) *redactor {
	if w == nil {
		w = io.Discard
	}
	r := &redactor{w: w, matches: map[int]int{}}
	for _, value := range values {
		if len(value) != 0 && !slices.ContainsFunc(r.values, func(v []byte) bool { return bytes.Equal(v, value) }) {
			r.values = append(r.values, value)
			r.fail = append(r.fail, kmpFailure(value))
		}
	}
	r.state = make([]int, len(r.values))
	return r
}

// kmpFailure returns, for each prefix length, its longest proper border.
func kmpFailure(v []byte) []int {
	f := make([]int, len(v)+1)
	for i, k := 1, 0; i < len(v); i++ {
		for k > 0 && v[i] != v[k] {
			k = f[k]
		}
		if v[i] == v[k] {
			k++
		}
		f[i+1] = k
	}
	return f
}

func (r *redactor) Write(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	for _, c := range p {
		pos := r.base + len(r.pending)
		r.pending = append(r.pending, c)
		for j, v := range r.values {
			s := r.state[j]
			for s > 0 && v[s] != c {
				s = r.fail[j][s]
			}
			if v[s] == c {
				s++
			}
			if s == len(v) {
				if start := pos + 1 - s; s > r.matches[start] {
					r.matches[start] = s
				}
				s = r.fail[j][s]
			}
			r.state[j] = s
		}
	}
	if r.emit(false); r.err != nil {
		return 0, r.err
	}
	return len(p), nil
}

// emit decides every position before the earliest possible value start. At
// EOF no value can still grow, so everything is decided.
func (r *redactor) emit(eof bool) {
	end := r.base + len(r.pending)
	live := end
	if !eof {
		for _, s := range r.state {
			live = min(live, end-s)
		}
	}
	var out []byte
	i := r.base
	for i < live {
		if n := r.matches[i]; n > 0 {
			out = append(out, redactionMarker...)
			i += n
			continue
		}
		out = append(out, r.pending[i-r.base])
		i++
	}
	for start := range r.matches {
		if start < i {
			delete(r.matches, start)
		}
	}
	n := copy(r.pending, r.pending[i-r.base:])
	clear(r.pending[n:])
	r.pending, r.base = r.pending[:n], i
	if len(out) != 0 {
		_, r.err = r.w.Write(out)
	}
}

// Flush decides the held bytes once the stream has ended.
func (r *redactor) Flush() error {
	if r.err == nil {
		r.emit(true)
	}
	clear(r.pending)
	r.pending = nil
	return r.err
}

func (r *redactor) wipe() {
	clear(r.pending)
	clear(r.state)
	r.pending, r.values, r.fail, r.matches = nil, nil, nil, nil
}

// childEnvironment combines the neutral base with the Task's delivered
// variables. A delivered variable replaces a base entry of the same name.
func childEnvironment(delivered []string) []string {
	names := map[string]bool{}
	for _, entry := range delivered {
		name, _, _ := strings.Cut(entry, "=")
		names[environmentKey(name)] = true
	}
	env := []string{}
	for _, entry := range baseEnvironment(environmentKey) {
		name, _, _ := strings.Cut(entry, "=")
		if !names[environmentKey(name)] {
			env = append(env, entry)
		}
	}
	return append(env, delivered...)
}
