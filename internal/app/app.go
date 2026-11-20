// Package app is the shared application core used by every user interface.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Origin  string `json:"origin,omitempty"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string        { return e.Message }
func Fail(code, message string) error { return &Error{Code: code, Message: message} }
func Classify(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	switch {
	case errors.Is(err, store.ErrNotInitialized):
		return &Error{Code: "not-initialized", Message: err.Error()}
	case errors.Is(err, store.ErrUnsupportedSchema):
		return &Error{Code: "unsupported-schema", Message: err.Error()}
	}
	var sqliteErr interface{ Code() int }
	if errors.As(err, &sqliteErr) && (sqliteErr.Code()&255 == 5 || sqliteErr.Code()&255 == 6) {
		return &Error{Code: "store-busy", Message: "local store is busy; retry the command"}
	}
	return &Error{Code: "store-error", Message: err.Error()}
}

type Workspace struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CreatedAt string `json:"createdAt"`
}
type Project struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt"`
}
type Binding struct {
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	DeviceID     string `json:"deviceId"`
	Path         string `json:"path"`
	CreatedAt    string `json:"createdAt"`
	MembershipID string `json:"membershipId,omitempty"`
	RepositoryID string `json:"repositoryId,omitempty"`
}
type InitResult struct {
	Workspace Workspace `json:"workspace"`
	Device    Device    `json:"device"`
	Created   bool      `json:"created"`
}
type BindResult struct {
	Project Project `json:"project"`
	Binding Binding `json:"binding"`
	Created bool    `json:"created"`
}
type ListResult struct {
	Projects []Project `json:"projects"`
}
type InspectResult struct {
	Project    Project     `json:"project"`
	Bindings   []Binding   `json:"bindings"`
	Resolution *Resolution `json:"resolution"`
}
type App struct {
	DataDir   string
	Providers provider.Services
	// Trust decides whether a credentialed execution definition is enabled on
	// this device. Nil refuses every credentialed run.
	Trust ExecutionTrust
}

func now() string            { return time.Now().UTC().Format(time.RFC3339Nano) }
func newID() (string, error) { u, e := uuid.NewRandom(); return u.String(), e }

func (a App) Init(ctx context.Context) (InitResult, error) {
	var r InitResult
	wid, e := newID()
	if e != nil {
		return r, e
	}
	did, e := newID()
	if e != nil {
		return r, e
	}
	name, e := os.Hostname()
	if e != nil {
		return r, e
	}
	stamp := now()
	db, e := store.Open(ctx, a.DataDir, true)
	if e != nil {
		return r, e
	}
	defer db.Close()
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		err := c.QueryRowContext(ctx, "SELECT id,created_at FROM workspace WHERE singleton=1").Scan(&r.Workspace.ID, &r.Workspace.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = c.ExecContext(ctx, "INSERT INTO workspace VALUES(1,?,?)", wid, stamp); err != nil {
				return err
			}
			if _, err = c.ExecContext(ctx, "INSERT INTO device VALUES(1,?,?,?,?,?)", did, name, runtime.GOOS, runtime.GOARCH, stamp); err != nil {
				return err
			}
			r.Workspace = Workspace{wid, stamp}
			r.Created = true
		} else if err != nil {
			return err
		}
		return c.QueryRowContext(ctx, "SELECT id,name,os,arch,created_at FROM device WHERE singleton=1").Scan(&r.Device.ID, &r.Device.Name, &r.Device.OS, &r.Device.Arch, &r.Device.CreatedAt)
	})
	return r, e
}

func (a App) openSubscribedProject(ctx context.Context, selector, cwd string) (*sql.DB, Project, error) {
	result, err := a.InspectProject(ctx, selector, cwd)
	if err != nil {
		return nil, Project{}, err
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return nil, Project{}, err
	}
	if err = requireSubscription(ctx, db, result.Project.ID); err != nil {
		db.Close()
		return nil, Project{}, err
	}
	return db, result.Project, nil
}

func canonical(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", Fail("invalid-path", e.Error())
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return "", Fail("invalid-path", e.Error())
	}
	info, e := os.Stat(abs)
	if e != nil {
		return "", Fail("invalid-path", e.Error())
	}
	if !info.IsDir() {
		return "", Fail("invalid-path", "path must be a directory")
	}
	// Resolve actual directory-entry casing only on platforms with commonly
	// case-insensitive filesystems. SameFile also protects case-sensitive volumes.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		parts := []string{}
		current := abs
		for {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			name := filepath.Base(current)
			target, err := os.Stat(current)
			if err != nil {
				return "", Fail("invalid-path", err.Error())
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				return "", Fail("invalid-path", err.Error())
			}
			for _, entry := range entries {
				if strings.EqualFold(entry.Name(), name) {
					candidate, err := os.Stat(filepath.Join(parent, entry.Name()))
					if err == nil && os.SameFile(target, candidate) {
						name = entry.Name()
						break
					}
				}
			}
			parts = append(parts, name)
			current = parent
		}
		for i := len(parts) - 1; i >= 0; i-- {
			current = filepath.Join(current, parts[i])
		}
		abs = current
	}
	return abs, nil
}
func contains(parent, child string) bool {
	rel, e := filepath.Rel(parent, child)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var slugRuns = regexp.MustCompile(`[^a-z0-9]+`)
var validSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (a App) BindProject(ctx context.Context, path, slug string) (BindResult, error) {
	return a.BindProjectWith(ctx, path, slug, "", "", nil)
}
func (a App) BindProjectWith(ctx context.Context, path, slug, selector, repository string, remotes []string) (BindResult, error) {

	release, lockErr := a.syncOwner(ctx)
	if lockErr != nil {
		return BindResult{}, lockErr
	}
	defer release()
	var r BindResult
	path, e := a.bindingPath(path)
	if e != nil {
		return r, e
	}
	if selector != "" && slug != "" {
		return r, Fail("usage", "project and slug cannot be combined")
	}
	git := discoverGit(ctx, path)
	if repository != "" && git.CommonDir == "" {
		return r, Fail("invalid-repository", "repository requires a Git checkout")
	}
	aliases := []string{}
	for _, raw := range remotes {
		remote, err := normalizeRemote(raw)
		if err != nil {
			return r, err
		}
		aliases = append(aliases, remote)
	}
	explicit := slug != ""
	if explicit && (len(slug) > 63 || !validSlug.MatchString(slug)) {
		return r, Fail("invalid-slug", "slug must be at most 63 lowercase letters, digits or single hyphens")
	}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return r, e
	}
	defer db.Close()
	if e = a.prepareBaseline(ctx, db); e != nil {
		return r, e
	}
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		if err := bindProject(ctx, c, &r, path, slug, selector, repository, git, aliases); err != nil {
			return err
		}
		return queueChanges(ctx, c)
	})
	return r, e
}

func (a App) bindingPath(path string) (string, error) {
	path, err := canonical(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(a.DataDir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if contains(path, root) {
		return "", Fail("invalid-path", "application data directory must be outside the bound directory")
	}
	return path, nil
}

func defaultProjectSlug(path string) string {
	slug := strings.Trim(slugRuns.ReplaceAllString(strings.ToLower(filepath.Base(path)), "-"), "-")
	if len(slug) > 63 {
		slug = strings.TrimRight(slug[:63], "-")
	}
	if slug == "" {
		slug = "project"
	}
	return slug
}

// bindProject saves the Project and checkout association in the caller's transaction.
func bindProject(ctx context.Context, c *sql.Conn, r *BindResult, path, slug, selector, repository string, git GitLocation, aliases []string) error {
	explicit := slug != ""
	if !explicit {
		slug = defaultProjectSlug(path)
	}
	pid, e := newID()
	if e != nil {
		return e
	}
	bid, e := newID()
	if e != nil {
		return e
	}
	stamp := now()
	var did string
	if err := c.QueryRowContext(ctx, "SELECT id FROM device WHERE singleton=1").Scan(&did); err != nil {
		return err
	}
	err := c.QueryRowContext(ctx, `SELECT p.id,p.slug,p.created_at,b.id,b.project_id,b.device_id,b.path,b.created_at FROM checkout_bindings b JOIN projects p ON p.id=b.project_id WHERE b.device_id=? AND b.path=?`, did, path).Scan(&r.Project.ID, &r.Project.Slug, &r.Project.CreatedAt, &r.Binding.ID, &r.Binding.ProjectID, &r.Binding.DeviceID, &r.Binding.Path, &r.Binding.CreatedAt)
	if err == nil {
		if (explicit && slug != r.Project.Slug) || (selector != "" && selector != r.Project.ID && selector != r.Project.Slug) {
			return Fail("binding-conflict", "directory is already bound to a different slug")
		}
		if err := registerGit(ctx, c, r, git, repository, aliases); err != nil {
			return err
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if selector != "" {
		p, err := selectProject(ctx, c, selector)
		if err != nil {
			return err
		}
		pid, slug, stamp = p.ID, p.Slug, p.CreatedAt
	}
	var count int
	if err = c.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE slug=?", slug).Scan(&count); err != nil {
		return err
	}
	if count > 0 && selector == "" {
		if explicit {
			return Fail("slug-conflict", "slug is already in use")
		}
		slug = strings.TrimRight(slug[:min(54, len(slug))], "-") + "-" + pid[:8]
	}
	r.Project = Project{pid, slug, stamp}
	r.Binding = Binding{ID: bid, ProjectID: pid, DeviceID: did, Path: path, CreatedAt: now()}
	if selector == "" {
		if _, err = c.ExecContext(ctx, "INSERT INTO projects VALUES(?,?,?)", pid, slug, stamp); err != nil {
			return err
		}
	}
	if _, err = c.ExecContext(ctx, "INSERT INTO checkout_bindings(id,project_id,device_id,path,created_at) VALUES(?,?,?,?,?)", bid, pid, did, path, r.Binding.CreatedAt); err != nil {
		return err
	}
	r.Created = true
	if err := registerGit(ctx, c, r, git, repository, aliases); err != nil {
		return err
	}
	return nil
}

func (a App) ListProjects(ctx context.Context) (ListResult, error) {
	return a.ListProjectsAll(ctx, false)
}
func (a App) ListProjectsAll(ctx context.Context, all bool) (ListResult, error) {
	r := ListResult{Projects: []Project{}}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return r, e
	}
	defer db.Close()
	return listProjects(ctx, db, all)
}

func listProjects(ctx context.Context, db *sql.DB, all bool) (ListResult, error) {
	r := ListResult{Projects: []Project{}}
	query := "SELECT id,slug,created_at FROM projects WHERE id NOT IN (SELECT id FROM sync_tombstones)"
	if !all {
		query += " AND id NOT IN (SELECT project_id FROM project_subscriptions WHERE subscribed=0)"
	}
	rows, e := db.QueryContext(ctx, query+" ORDER BY slug,id")
	if e != nil {
		return r, e
	}
	defer rows.Close()
	for rows.Next() {
		var p Project
		if e = rows.Scan(&p.ID, &p.Slug, &p.CreatedAt); e != nil {
			return r, e
		}
		r.Projects = append(r.Projects, p)
	}
	return r, rows.Err()
}
func (a App) InspectProject(ctx context.Context, selector, cwd string) (InspectResult, error) {
	r := InspectResult{Bindings: []Binding{}}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return r, e
	}
	defer db.Close()
	resolution, err := resolveProject(ctx, db, selector, cwd)
	r.Resolution = &resolution
	if err != nil {
		return r, err
	}
	selector = resolution.Project.ID
	e = db.QueryRowContext(ctx, "SELECT id,slug,created_at FROM projects WHERE id=? OR slug=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END LIMIT 1", selector, selector, selector).Scan(&r.Project.ID, &r.Project.Slug, &r.Project.CreatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return r, Fail("project-not-found", fmt.Sprintf("project %q was not found", selector))
	}
	if e != nil {
		return r, e
	}
	rows, e := db.QueryContext(ctx, "SELECT b.id,b.project_id,b.device_id,b.path,b.created_at,COALESCE(b.membership_id,''),COALESCE(m.repository_id,'') FROM checkout_bindings b LEFT JOIN memberships m ON m.id=b.membership_id WHERE b.project_id=? ORDER BY b.path", r.Project.ID)
	if e != nil {
		return r, e
	}
	defer rows.Close()
	for rows.Next() {
		var b Binding
		if e = rows.Scan(&b.ID, &b.ProjectID, &b.DeviceID, &b.Path, &b.CreatedAt, &b.MembershipID, &b.RepositoryID); e != nil {
			return r, e
		}
		r.Bindings = append(r.Bindings, b)
	}
	return r, rows.Err()
}
