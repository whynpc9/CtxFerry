package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type OnboardingState struct {
	Step        string             `json:"step"`
	Directory   string             `json:"directory,omitempty"`
	ProjectID   string             `json:"projectId,omitempty"`
	DocumentIDs []string           `json:"documentIds"`
	Preview     *OnboardingPreview `json:"preview,omitempty"`
	Doctor      *DoctorReport      `json:"doctor,omitempty"`
}

type OnboardingSelection struct {
	Project, Repository string
}

type OnboardingPreview struct {
	ID           string      `json:"id,omitempty"`
	Source       string      `json:"source"`
	Git          GitLocation `json:"git"`
	Project      *Project    `json:"project"`
	NewSlug      string      `json:"newSlug,omitempty"`
	RepositoryID string      `json:"repositoryId,omitempty"`
	Candidates   []Candidate `json:"candidates"`
}

// StartOnboarding stores the selected directory. Its preview does not bind it.
func (a App) StartOnboarding(ctx context.Context, path string, selection OnboardingSelection) (OnboardingState, error) {
	path, err := a.bindingPath(path)
	if err != nil {
		return OnboardingState{}, err
	}
	if _, err = a.Init(ctx); err != nil {
		return OnboardingState{}, err
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return OnboardingState{}, err
	}
	defer db.Close()
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		state, err := readOnboarding(ctx, c)
		if err != nil {
			return err
		}
		if state.Step == "not-started" {
			_, err = c.ExecContext(ctx, "INSERT INTO onboarding(singleton,directory) VALUES(1,?)", path)
			return err
		}
		if state.Directory != path {
			return Fail("onboarding-started", "onboarding already has a directory; run onboard status to resume")
		}
		return nil
	})
	if err != nil {
		return OnboardingState{}, err
	}
	return a.OnboardingStatus(ctx, selection)
}

// OnboardingStatus reads local state and recalculates the preview without writing.
func (a App) OnboardingStatus(ctx context.Context, selection OnboardingSelection) (OnboardingState, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if errors.Is(err, store.ErrNotInitialized) {
		return newOnboardingState(), nil
	}
	if err != nil {
		return OnboardingState{}, err
	}
	defer db.Close()
	state, err := readOnboarding(ctx, db)
	if err != nil || state.Step != "preview" {
		return state, err
	}
	path, err := a.onboardingPath(state)
	if err != nil {
		return state, err
	}
	preview, err := previewOnboarding(ctx, db, path, selection, discoverGit(ctx, path))
	state.Preview = &preview
	return state, err
}

func newOnboardingState() OnboardingState {
	return OnboardingState{Step: "not-started", DocumentIDs: []string{}}
}

func readOnboarding(ctx context.Context, q projectReader) (OnboardingState, error) {
	state := newOnboardingState()
	var completed bool
	err := q.QueryRowContext(ctx, "SELECT directory,project_id,completed FROM onboarding WHERE singleton=1").
		Scan(&state.Directory, &state.ProjectID, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Step = "preview"
	if state.ProjectID == "" {
		return state, nil
	}
	state.Step = "notes"
	if completed {
		state.Step = "complete"
	}
	rows, err := q.QueryContext(ctx, "SELECT document_id FROM onboarding_notes ORDER BY rowid")
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return state, err
		}
		state.DocumentIDs = append(state.DocumentIDs, id)
	}
	return state, rows.Err()
}

func (a App) onboardingPath(state OnboardingState) (string, error) {
	path, err := a.bindingPath(state.Directory)
	if err == nil && path != state.Directory {
		return "", Fail("invalid-path", "the selected directory now resolves to a different path")
	}
	return path, err
}

func previewOnboarding(ctx context.Context, q projectReader, path string, selection OnboardingSelection, git GitLocation) (OnboardingPreview, error) {
	resolution, err := resolveProjectAt(ctx, q, "", Resolution{
		Directory: path, Git: git, Source: "unbound", Candidates: []Candidate{},
	}, true)
	if err != nil && Classify(err).Code != "project-unbound" {
		return OnboardingPreview{}, err
	}
	preview := OnboardingPreview{
		Source: resolution.Source, Git: git, Project: resolution.Project, Candidates: resolution.Candidates,
	}
	if selection.Project != "" {
		project, err := selectProject(ctx, q, selection.Project)
		if err != nil {
			return preview, err
		}
		preview.Project, preview.Source = &project, "explicit"
	}
	switch {
	case preview.Project != nil && selection.Repository != "":
		if git.Status != "available" {
			return preview, Fail("git-unavailable", "Git discovery is required to select a Repository")
		}
		var exists bool
		if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM repositories WHERE id=?)", selection.Repository).Scan(&exists); err != nil {
			return preview, err
		}
		if !exists {
			return preview, Fail("repository-not-found", "Repository was not found")
		}
		preview.RepositoryID = selection.Repository
	case preview.Project != nil:
		for _, candidate := range preview.Candidates {
			// The matching membership must cover the chosen directory.
			if candidate.Project.ID != preview.Project.ID ||
				!contains(git.CheckoutRoot, path) || !contains(filepath.Join(git.CheckoutRoot, filepath.FromSlash(candidate.Subdirectory)), path) {
				continue
			}
			if preview.RepositoryID != "" && preview.RepositoryID != candidate.RepositoryID {
				preview.RepositoryID = ""
				preview.Source = "repository-ambiguous"
				return preview, nil
			}
			preview.RepositoryID = candidate.RepositoryID
		}
	case len(preview.Candidates) != 0:
		preview.Source = "choose-project"
		return preview, nil
	default:
		if selection.Repository != "" {
			return preview, Fail("usage", "select an existing Project when choosing a Repository")
		}
		preview.Source = "new-project"
		preview.NewSlug, err = availableOnboardingSlug(ctx, q, defaultProjectSlug(path))
		if err != nil {
			return preview, err
		}
	}
	// Git's raw remote URLs never leave discovery. Include its normalized evidence
	// in the confirmation so a change between preview and bind needs a new review.
	wire, err := json.Marshal(struct {
		Directory string
		DeviceID  string
		Remotes   []string
		Preview   OnboardingPreview
	}{path, resolution.Device.ID, git.remotes, preview})
	if err != nil {
		return preview, err
	}
	preview.ID = digest(wire)
	return preview, nil
}

func availableOnboardingSlug(ctx context.Context, q queryer, base string) (string, error) {
	slug := base
	for n := 2; ; n++ {
		var used bool
		if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE slug=?)", slug).Scan(&used); err != nil {
			return "", err
		}
		if !used {
			return slug, nil
		}
		suffix := "-" + strconv.Itoa(n)
		slug = strings.TrimRight(base[:min(len(base), 63-len(suffix))], "-") + suffix
	}
}

func (a App) ConfirmOnboardingBinding(ctx context.Context, selection OnboardingSelection, previewID string) (OnboardingState, error) {
	var state OnboardingState
	if previewID == "" {
		return state, Fail("usage", "confirm the Project with the preview ID from onboard status")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return state, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return state, err
	}
	defer db.Close()
	state, err = readOnboarding(ctx, db)
	if err != nil {
		return state, err
	}
	if state.Step != "preview" {
		return state, Fail("onboarding-step", "project preview is not pending; run onboard status")
	}
	path, err := a.onboardingPath(state)
	if err != nil {
		return state, err
	}
	git := discoverGit(ctx, path)
	err = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		preview, err := previewOnboarding(ctx, c, path, selection, git)
		if err != nil {
			return false, err
		}
		if preview.ID != previewID {
			return false, Fail("preview-changed", "project preview changed or is ambiguous; run onboard status")
		}
		projectID := ""
		if preview.Project != nil {
			projectID = preview.Project.ID
		}
		var bound BindResult
		if err = bindProject(ctx, c, &bound, path, preview.NewSlug, projectID, preview.RepositoryID, git, nil); err != nil {
			return false, err
		}
		if _, err = c.ExecContext(ctx, "UPDATE onboarding SET project_id=? WHERE singleton=1", bound.Project.ID); err != nil {
			return false, err
		}
		state.Step, state.ProjectID = "notes", bound.Project.ID
		return true, nil
	})
	return state, err
}

// FinishOnboarding runs the same doctor used by the standalone command.
// Repeating finish refreshes the diagnostic while preserving completion.
func (a App) FinishOnboarding(ctx context.Context) (OnboardingState, error) {
	var state OnboardingState
	release, err := a.syncOwner(ctx)
	if err != nil {
		return state, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return state, err
	}
	defer db.Close()
	state, err = readOnboarding(ctx, db)
	if err != nil {
		return state, err
	}
	if state.ProjectID == "" {
		return state, Fail("onboarding-step", "confirm the Project before finishing onboarding")
	}
	report := a.Doctor(ctx, "", state.Directory)
	if err = ctx.Err(); err != nil {
		return state, err
	}
	if state.Step != "complete" {
		if _, err = db.ExecContext(ctx, "UPDATE onboarding SET completed=1 WHERE singleton=1"); err != nil {
			return state, err
		}
	}
	state.Step, state.Doctor = "complete", &report
	return state, nil
}
