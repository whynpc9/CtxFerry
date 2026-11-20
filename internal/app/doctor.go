package app

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"time"

	"github.com/whynpc9/CtxFerry/internal/platform"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type DoctorCheck struct {
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

type DoctorReport struct {
	ObservedAt       time.Time          `json:"observedAt"`
	OS               string             `json:"os"`
	Arch             string             `json:"arch"`
	CLI              DoctorCheck        `json:"cli"`
	Catalog          DoctorCheck        `json:"catalog"`
	DirectoryBinding DoctorBinding      `json:"directoryBinding"`
	PathResolution   DoctorPaths        `json:"pathResolution"`
	GitAuth          DoctorGitAuth      `json:"gitAuth"`
	ProviderAuth     DoctorProviders    `json:"providerAuth"`
	LastSync         DoctorSync         `json:"lastSync"`
	Dependencies     []DoctorDependency `json:"dependencies"`
	CredentialStore  DoctorCheck        `json:"credentialStorage"`
	BackgroundSync   DoctorCheck        `json:"backgroundSync"`
	Resources        DoctorResources    `json:"resources"`
}

type DoctorBinding struct {
	DoctorCheck
	ProjectID string `json:"projectId"`
	Source    string `json:"source"`
}

type DoctorPaths struct {
	DoctorCheck
	Directory        string      `json:"directory"`
	GitCheckout      string      `json:"gitCheckout"`
	Checkout         string      `json:"checkout"`
	WorkingDirectory DoctorCheck `json:"workingDirectory"`
}

type DoctorGitAuth struct {
	DoctorCheck
	LastSyncState string   `json:"lastSyncState"`
	Mechanisms    []string `json:"mechanisms"`
}

type DoctorProviders struct {
	DoctorCheck
	Profiles []DoctorProvider `json:"profiles"`
}

type DoctorProvider struct {
	DoctorCheck
	Profile       provider.Profile            `json:"profile"`
	StoredSession provider.CredentialEvidence `json:"storedSession"`
	Headless      string                      `json:"headless"`
}

type DoctorSync struct {
	DoctorCheck
	State             string     `json:"state"`
	LastSyncAt        *time.Time `json:"lastSyncAt"`
	Pending           *int       `json:"pending"`
	StaleAfterSeconds int        `json:"staleAfterSeconds"`
}

type DoctorDependency struct {
	DoctorCheck
	Name string `json:"name"`
	Use  string `json:"use"`
}

type DoctorResources struct {
	DoctorCheck
	Items []DoctorResource `json:"items"`
}

type DoctorResource struct {
	ID              string     `json:"id"`
	Route           string     `json:"route"`
	TCPReachability string     `json:"tcpReachability"`
	TCPObservedAt   *time.Time `json:"tcpObservedAt"`
	ServiceIdentity string     `json:"serviceIdentity"`
}

const syncStaleAfter = 24 * time.Hour

// Doctor only collects local evidence. In particular, normal sync and provider
// authentication APIs can invoke helpers or access credentials and are not probes.
func (a App) Doctor(ctx context.Context, selector, cwd string) DoctorReport {
	unknown := DoctorCheck{"unknown", "Local catalog evidence is unavailable"}
	r := DoctorReport{
		ObservedAt: time.Now().UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		CLI:              DoctorCheck{"available", "CtxFerry CLI is running"},
		DirectoryBinding: DoctorBinding{DoctorCheck: unknown},
		PathResolution:   DoctorPaths{WorkingDirectory: unknown},
		GitAuth:          DoctorGitAuth{DoctorCheck: DoctorCheck{"unknown", "Live Git authentication is unverified"}, Mechanisms: gitAuthMechanisms()},
		ProviderAuth:     DoctorProviders{DoctorCheck: unknown, Profiles: []DoctorProvider{}},
		LastSync:         DoctorSync{DoctorCheck: unknown, State: "unknown", StaleAfterSeconds: int(syncStaleAfter.Seconds())},
		Dependencies: []DoctorDependency{
			doctorDependency("git", "Git checkout discovery and sync"),
			doctorDependency("gh", "Optional GitHub metadata authentication via GitHub CLI"),
			doctorDependency("infisical", "Optional adoption of a personal Infisical CLI session"),
			doctorDependency("bw", "Bitwarden Password Manager vault status and reads"),
		},
		BackgroundSync: a.backgroundSyncStatus(ctx),
		Resources:      DoctorResources{DoctorCheck: unknown, Items: []DoctorResource{}},
	}
	if r.Dependencies[0].Status == "unavailable" {
		r.GitAuth.DoctorCheck = DoctorCheck{"unavailable", "Git was not found on PATH"}
	}

	loc := Resolution{Source: "unbound", Candidates: []Candidate{}}
	if cwd != "" {
		loc.Directory, _ = canonical(cwd)
	}
	if loc.Directory == "" {
		r.PathResolution.DoctorCheck = DoctorCheck{"unavailable", "Current directory could not be resolved"}
	} else {
		loc.Git = discoverGitLocation(ctx, loc.Directory, false)
		r.PathResolution.DoctorCheck = DoctorCheck{"available", "Current directory resolved"}
		r.PathResolution.Directory = loc.Directory
		r.PathResolution.GitCheckout = loc.Git.CheckoutRoot
	}

	if a.DataDir == "" {
		r.Catalog = DoctorCheck{"unknown", "Could not resolve the application data directory"}
		r.CredentialStore, _ = a.doctorCredentialStorage(ctx, nil)
		return r
	}
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		r.Catalog = doctorCatalogFailure(err)
		if errors.Is(err, store.ErrNotInitialized) {
			r.DirectoryBinding.DoctorCheck = DoctorCheck{"unavailable", "No local workspace is initialized"}
		}
		r.CredentialStore, _ = a.doctorCredentialStorage(ctx, nil)
		return r
	}
	defer db.Close()
	r.Catalog = DoctorCheck{"available", "Read the local catalog"}
	r.readSync(ctx, db)

	if loc.Directory == "" {
		r.DirectoryBinding.DoctorCheck = DoctorCheck{"unknown", "Current directory could not be resolved"}
		r.CredentialStore, _ = a.doctorCredentialStorage(ctx, nil)
		return r
	}
	a.doctorProject(ctx, db, selector, loc, &r)
	return r
}

func doctorCatalogFailure(err error) DoctorCheck {
	switch {
	case errors.Is(err, store.ErrNotInitialized):
		return DoctorCheck{"unavailable", "No local workspace is initialized"}
	case errors.Is(err, store.ErrMigrationRequired):
		return DoctorCheck{"unavailable", "Local catalog requires a schema migration"}
	case errors.Is(err, store.ErrUnsupportedSchema):
		return DoctorCheck{"unavailable", "Local catalog schema is newer than this CLI"}
	default:
		return DoctorCheck{"unknown", "Could not read the local catalog"}
	}
}

func doctorDependency(name, use string) DoctorDependency {
	if _, err := exec.LookPath(name); err != nil {
		return DoctorDependency{DoctorCheck{"unavailable", "Executable could not be resolved on PATH"}, name, use}
	}
	return DoctorDependency{DoctorCheck{"available", "Executable resolved on PATH"}, name, use}
}

func gitAuthMechanisms() []string {
	out := []string{}
	if _, present := os.LookupEnv("SSH_AUTH_SOCK"); present {
		out = append(out, "ssh-agent-socket-configured")
	}
	_, gh := os.LookupEnv("GH_TOKEN")
	_, github := os.LookupEnv("GITHUB_TOKEN")
	if gh || github {
		out = append(out, "github-metadata-token-environment-configured")
	}
	return out
}

func (r *DoctorReport) readSync(ctx context.Context, db *sql.DB) {
	local, err := readSyncStatus(ctx, db)
	if err != nil {
		if Classify(err).Code == "sync-disabled" {
			r.LastSync.State = "disabled"
			r.LastSync.DoctorCheck = DoctorCheck{"unavailable", "Sync is not configured"}
			if r.GitAuth.Status == "unknown" {
				r.GitAuth.DoctorCheck = DoctorCheck{"unknown", "No sync remote is configured for Git authentication"}
			}
		} else {
			r.LastSync.DoctorCheck = DoctorCheck{"unknown", "Could not read local sync status"}
		}
		return
	}
	r.LastSync.State, r.LastSync.Pending = local.State, &local.Pending
	r.GitAuth.LastSyncState = local.State
	if r.GitAuth.Status == "unknown" {
		switch local.State {
		case "auth-required", "unlock-required":
			r.GitAuth.DoctorCheck = DoctorCheck{"unknown", "Local sync recorded an authentication or unlock failure; current Git authentication is unverified"}
		default:
			r.GitAuth.DoctorCheck = DoctorCheck{"unknown", "Sync is configured; current Git authentication is unverified"}
		}
	}
	if local.LastSyncAt == nil {
		r.LastSync.DoctorCheck = DoctorCheck{"unknown", "No successful sync is recorded"}
		return
	}
	stamp, err := time.Parse(time.RFC3339Nano, *local.LastSyncAt)
	if err != nil {
		r.LastSync.DoctorCheck = DoctorCheck{"unknown", "Stored sync timestamp is invalid"}
		return
	}
	stamp = stamp.UTC()
	r.LastSync.LastSyncAt = &stamp
	switch {
	case stamp.After(r.ObservedAt):
		r.LastSync.DoctorCheck = DoctorCheck{"unknown", "Stored sync timestamp is ahead of this device's clock"}
	case r.ObservedAt.Sub(stamp) >= syncStaleAfter:
		r.LastSync.DoctorCheck = DoctorCheck{"stale", "Last successful sync was at least 24 hours ago"}
	default:
		r.LastSync.DoctorCheck = DoctorCheck{"available", "Successful sync recorded within the last 24 hours"}
	}
}

func (a App) doctorProject(ctx context.Context, db *sql.DB, selector string, loc Resolution, r *DoctorReport) {
	// The current directory's binding is independent of the explicit selector.
	binding, err := resolveProjectAt(ctx, db, "", loc, true)
	r.DirectoryBinding.Source = binding.Source
	switch {
	case err == nil:
		r.DirectoryBinding.Status = "available"
		r.DirectoryBinding.Evidence = "Directory resolves through a local binding"
		r.DirectoryBinding.ProjectID = binding.Project.ID
	case Classify(err).Code == "project-unbound" && loc.Git.Status != "unavailable":
		r.DirectoryBinding.DoctorCheck = DoctorCheck{"unavailable", "No unique local binding matched"}
	case Classify(err).Code == "project-unbound":
		r.DirectoryBinding.DoctorCheck = DoctorCheck{"unknown", "No direct path binding matched and Git worktree discovery was unavailable"}
	default:
		r.DirectoryBinding.DoctorCheck = DoctorCheck{"unknown", "Could not read the current directory binding"}
	}

	selected := binding
	if selector != "" {
		selected, err = resolveProjectAt(ctx, db, selector, loc, true)
	}
	if err != nil {
		r.CredentialStore, _ = a.doctorCredentialStorage(ctx, nil)
		r.ProviderAuth.Evidence = "A resolved Project is required to identify provider profiles"
		r.Resources.Evidence = "A resolved Project is required to inspect resource routes"
		r.PathResolution.WorkingDirectory.Evidence = "A resolved Project is required to determine the working directory"
		return
	}
	config, configErr := configurationAt(ctx, db, selected)
	if configErr != nil {
		r.CredentialStore, _ = a.doctorCredentialStorage(ctx, nil)
		r.ProviderAuth.Evidence = "Could not access Project configuration"
		r.Resources.Evidence = "Could not access Project configuration"
		r.PathResolution.WorkingDirectory.Evidence = "Could not access Project configuration"
		return
	}

	r.PathResolution.readWorkingDirectory(ctx, db, config)
	r.Resources = doctorResources(ctx, db, selected.Project, loc.Directory)
	r.ProviderAuth, r.CredentialStore = a.doctorProviderAuth(ctx, db, config)
}

func (p *DoctorPaths) readWorkingDirectory(ctx context.Context, db *sql.DB, loc configurationLocation) {
	if loc.checkout != nil {
		p.Checkout = loc.checkout.Path
	}
	explanation, err := explainConfigurationAt(ctx, db, loc, "")
	if err != nil {
		switch Classify(err).Code {
		case "invalid-config-path", "checkout-required":
			p.WorkingDirectory = DoctorCheck{"unavailable", "The effective workingDirectory cannot be resolved on this device"}
		default:
			p.WorkingDirectory = DoctorCheck{"unknown", "Could not read effective Project configuration"}
		}
		return
	}
	for _, value := range explanation.Values {
		if value.Field == "workingDirectory" {
			p.WorkingDirectory = DoctorCheck{"available", "The effective workingDirectory resolves on this device"}
			return
		}
	}
	if loc.checkout != nil {
		p.WorkingDirectory = DoctorCheck{"available", "The checkout resolves and no workingDirectory override is set"}
	} else {
		p.WorkingDirectory = DoctorCheck{"unavailable", "No local checkout or workingDirectory override matched"}
	}
}

func (a App) doctorCredentialStorage(ctx context.Context, keys []provider.SessionKey) (DoctorCheck, map[provider.SessionKey]provider.CredentialEvidence) {
	inspector, ok := a.Providers.Credentials.(provider.CredentialInspector)
	if !ok {
		if a.Providers.Credentials == nil {
			return DoctorCheck{"unavailable", "No secure credential backend is configured"}, nil
		}
		return DoctorCheck{"unknown", "Credential backend does not expose safe metadata inspection"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	state, evidence, entries := inspector.InspectCredentials(ctx, keys)
	return DoctorCheck{string(state), evidence}, entries
}

func (a App) doctorProviderAuth(ctx context.Context, db *sql.DB, loc configurationLocation) (DoctorProviders, DoctorCheck) {
	out := DoctorProviders{Profiles: []DoctorProvider{}}
	list, err := listProjectSecrets(ctx, db, loc.project)
	if err != nil {
		storage, _ := a.doctorCredentialStorage(ctx, nil)
		out.DoctorCheck = DoctorCheck{"unknown", "Could not read the Project's SecretBindings"}
		return out, storage
	}
	seen := map[provider.Profile]bool{}
	for _, binding := range list.Bindings {
		seen[binding.ProviderProfile] = true
	}
	profiles := make([]provider.Profile, 0, len(seen))
	for p := range seen {
		profiles = append(profiles, p)
	}
	sortProviderProfiles(profiles)
	keys := make([]provider.SessionKey, 0, len(profiles))
	for _, p := range profiles {
		keys = append(keys, provider.SessionKey{WorkspaceID: loc.workspace, DeviceID: loc.device, Profile: p})
	}
	storage, sessions := a.doctorCredentialStorage(ctx, keys)
	out.DoctorCheck = DoctorCheck{"unknown", "Live authentication is unverified; see the evidence for each provider profile"}
	if len(keys) == 0 {
		out.DoctorCheck = DoctorCheck{"unavailable", "No provider profiles are referenced by the Project's SecretBindings"}
	}
	for _, key := range keys {
		out.Profiles = append(out.Profiles, a.doctorProviderProfile(key, storage, sessions[key]))
	}
	return out, storage
}

func sortProviderProfiles(profiles []provider.Profile) {
	slices.SortFunc(profiles, func(a, b provider.Profile) int {
		return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Method, b.Method))
	})
}

func (a App) doctorProviderProfile(key provider.SessionKey, storage DoctorCheck, session provider.CredentialEvidence) DoctorProvider {
	if session == "" {
		session = provider.CredentialUnknown
	}
	p := DoctorProvider{Profile: key.Profile, StoredSession: session, Headless: "unknown"}
	if a.Providers.HasInjectedSession != nil {
		p.Headless = "not-configured"
		if a.Providers.HasInjectedSession(key) {
			p.Headless = "scoped-runtime-session-configured"
		}
	} else if a.Providers.InjectedSession == nil {
		p.Headless = "not-configured"
	}

	_, _, err := a.providerAdapter(key.Profile)
	switch {
	case err != nil:
		p.DoctorCheck = DoctorCheck{"unavailable", "Provider adapter or authentication method is unsupported"}
	case p.Headless == "scoped-runtime-session-configured":
		p.DoctorCheck = DoctorCheck{"unknown", "Scoped runtime session is configured; provider authentication is unverified"}
	case session == provider.CredentialPresent:
		p.DoctorCheck = DoctorCheck{"unknown", "Stored session entry exists; provider authentication is unverified"}
	case p.Headless == "unknown":
		p.DoctorCheck = DoctorCheck{"unknown", "Runtime session configuration could not be inspected"}
	case session == provider.CredentialAbsent:
		p.DoctorCheck = DoctorCheck{"unavailable", "No stored session or scoped runtime session is configured; login required"}
	case storage.Status == "unavailable" || storage.Status == "locked":
		p.DoctorCheck = DoctorCheck{"unavailable", "Secure storage is inaccessible and no scoped runtime session is configured"}
	default:
		p.DoctorCheck = DoctorCheck{"unknown", "Stored session presence and provider authentication are unverified"}
	}
	return p
}

func doctorResources(ctx context.Context, db *sql.DB, project *Project, cwd string) DoctorResources {
	out := DoctorResources{DoctorCheck: DoctorCheck{"available", "Read local resource routes and recorded TCP reachability"}, Items: []DoctorResource{}}
	list, err := listProjectResources(ctx, db, *project)
	if err != nil {
		out.DoctorCheck = DoctorCheck{"unknown", "Could not read Project resources"}
		return out
	}
	for _, resource := range list.Resources {
		fact := DoctorResource{ID: resource.ID, Route: "unknown", TCPReachability: "unknown", ServiceIdentity: "unknown"}
		detail, err := resourceDetail(ctx, db, project.ID, cwd, resource.ID)
		if err == nil {
			fact.Route = detail.Route.Status
			obs, err := latestServiceObservation(ctx, db, detail)
			if err == nil && obs != nil {
				stamp, err := time.Parse(time.RFC3339Nano, obs.ObservedAt)
				if err == nil {
					fact.TCPReachability, fact.TCPObservedAt = obs.Status, &stamp
				}
			}
		}
		out.Items = append(out.Items, fact)
	}
	return out
}

// Observe the current user's service manager without starting or installing a job.
func (a App) backgroundSyncStatus(ctx context.Context) DoctorCheck {
	service := platform.SyncServiceStatus(ctx, a.DataDir)
	switch {
	case service.Running:
		return DoctorCheck{"running", service.Message}
	case service.Installed:
		return DoctorCheck{"installed", service.Message}
	case service.Available:
		return DoctorCheck{"foreground-only", service.Message}
	default:
		return DoctorCheck{"unavailable", service.Message}
	}
}
