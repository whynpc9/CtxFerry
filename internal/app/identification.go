package app

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type GitLocation struct {
	CheckoutRoot string `json:"checkoutRoot,omitempty"`
	GitDir       string `json:"gitDir,omitempty"`
	CommonDir    string `json:"commonDir,omitempty"`
	Status       string `json:"status"`
	remotes      []string
}
type Candidate struct {
	Project      Project `json:"project"`
	RepositoryID string  `json:"repositoryId"`
	MembershipID string  `json:"membershipId"`
	Subdirectory string  `json:"subdirectory"`
	Evidence     string  `json:"evidence"`
}
type Resolution struct {
	Project    *Project    `json:"project"`
	Source     string      `json:"source"`
	Directory  string      `json:"directory"`
	Git        GitLocation `json:"git"`
	Device     Device      `json:"device"`
	Candidates []Candidate `json:"candidates"`
}

func discoveryGit(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	for _, name := range []string{"PATH", "PATHEXT", "HOME", "USERPROFILE", "SystemRoot", "WINDIR", "TMPDIR", "TMP", "TEMP"} {
		if value, present := os.LookupEnv(name); present {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return cmd
}

// Discovery never contacts a remote, invokes a shell, or trusts inherited Git locations.
func discoverGit(ctx context.Context, path string) GitLocation {
	return discoverGitLocation(ctx, path, true)
}

// A location-only check skips remote URLs, which may contain embedded credentials.
func discoverGitLocation(ctx context.Context, path string, includeRemotes bool) GitLocation {
	g := GitLocation{Status: "unavailable"}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		out, err := discoveryGit(ctx, path, args...).Output()
		return strings.TrimSpace(string(out)), err
	}
	root, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "not a git repository") {
			g.Status = "not-git"
		}
		return g
	}
	root, err = canonical(root)
	if err != nil {
		return g
	}
	gd, err := run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return g
	}
	common, err := run("rev-parse", "--git-common-dir")
	if err != nil {
		return g
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	common, err = canonical(common)
	if err != nil {
		return g
	}
	gd, err = canonical(gd)
	if err != nil {
		return g
	}
	g = GitLocation{CheckoutRoot: root, GitDir: gd, CommonDir: common, Status: "available"}
	if !includeRemotes {
		return g
	}
	names, err := run("remote")
	if err != nil {
		g.Status = "unavailable"
		return g
	}
	for _, name := range strings.Fields(names) {
		raw, err := run("config", "--get-all", "remote."+name+".url")
		if err != nil {
			continue
		}
		for _, v := range strings.Split(raw, "\n") {
			if n, e := normalizeRemote(v); e == nil {
				g.remotes = append(g.remotes, n)
			}
		}
	}
	return g
}

// Only conventional GitHub transports normalize. Credentials and custom endpoints
// are excluded; owner identity is retained so forks never collapse into upstream.
func normalizeRemote(raw string) (string, error) {
	invalid := func() (string, error) {
		return "", Fail("invalid-remote", "remote must be a credential-free conventional GitHub SSH or HTTPS URL")
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "ssh://git@github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	u, e := url.Parse(raw)
	if e != nil || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
		return invalid()
	}
	if u.Scheme == "https" {
		if u.User != nil {
			return invalid()
		}
	} else if u.Scheme == "ssh" {
		if u.User == nil || u.User.String() != "git" {
			return invalid()
		}
	} else {
		return invalid()
	}
	p := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		return invalid()
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return invalid()
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return invalid()
			}
		}
	}
	return "github.com/" + strings.ToLower(p), nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type projectReader interface {
	queryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func selectProject(ctx context.Context, q queryer, selector string) (Project, error) {
	var p Project
	e := q.QueryRowContext(ctx, "SELECT id,slug,created_at FROM projects WHERE (id=? OR slug=?) AND "+live("projects.id")+" ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END LIMIT 1", selector, selector, selector).Scan(&p.ID, &p.Slug, &p.CreatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return p, Fail("project-not-found", "selected Project was not found")
	}
	return p, e
}
func registerGit(ctx context.Context, c *sql.Conn, r *BindResult, g GitLocation, repository string, aliases []string) error {
	if g.Status != "available" {
		if repository != "" || len(aliases) > 0 {
			return Fail("git-unavailable", "Git discovery is required for repository registration")
		}
		return nil
	}
	var existing sql.NullString
	if e := c.QueryRowContext(ctx, `SELECT m.repository_id FROM checkout_bindings b LEFT JOIN memberships m ON m.id=b.membership_id WHERE b.id=?`, r.Binding.ID).Scan(&existing); e != nil {
		return e
	}
	if existing.Valid {
		if repository != "" && repository != existing.String {
			return Fail("binding-conflict", "binding already has a different Repository")
		}
		repository = existing.String
	}
	if repository == "" {
		rows, e := c.QueryContext(ctx, `SELECT DISTINCT m.repository_id FROM checkout_bindings b JOIN memberships m ON m.id=b.membership_id WHERE b.device_id=? AND b.git_common_dir=?`, r.Binding.DeviceID, g.CommonDir)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(ids) > 1 {
			return Fail("repository-ambiguous", "specify --repository for this common directory")
		}
		if len(ids) == 1 {
			repository = ids[0]
		} else {
			repository, e = newID()
			if e != nil {
				return e
			}
			if _, e = c.ExecContext(ctx, "INSERT INTO repositories VALUES(?,?)", repository, now()); e != nil {
				return e
			}
		}
	} else {
		var id string
		if e := c.QueryRowContext(ctx, "SELECT id FROM repositories WHERE id=?", repository).Scan(&id); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return Fail("repository-not-found", "Repository was not found")
			}
			return e
		}
	}
	rel, e := filepath.Rel(g.CheckoutRoot, r.Binding.Path)
	if e != nil || !contains(g.CheckoutRoot, r.Binding.Path) {
		return Fail("invalid-path", "binding must be inside checkout")
	}
	rel = filepath.ToSlash(rel)
	mid, e := newID()
	if e != nil {
		return e
	}
	if _, e = c.ExecContext(ctx, "INSERT OR IGNORE INTO memberships VALUES(?,?,?,?)", mid, r.Project.ID, repository, rel); e != nil {
		return e
	}
	if e = c.QueryRowContext(ctx, "SELECT id FROM memberships WHERE project_id=? AND repository_id=? AND subdirectory=?", r.Project.ID, repository, rel).Scan(&mid); e != nil {
		return e
	}
	if _, e = c.ExecContext(ctx, "UPDATE checkout_bindings SET membership_id=?,checkout_root=?,git_dir=?,git_common_dir=? WHERE id=?", mid, g.CheckoutRoot, g.GitDir, g.CommonDir, r.Binding.ID); e != nil {
		return e
	}
	for _, alias := range aliases {
		if _, e = c.ExecContext(ctx, "INSERT OR IGNORE INTO repository_aliases VALUES(?,?)", repository, alias); e != nil {
			return e
		}
	}
	r.Binding.MembershipID = mid
	r.Binding.RepositoryID = repository
	return nil
}

func (a App) AddRemote(ctx context.Context, repository, raw string) (map[string]string, error) {
	release, lockErr := a.syncOwner(ctx)
	if lockErr != nil {
		return nil, lockErr
	}
	defer release()

	remote, e := normalizeRemote(raw)
	if e != nil {
		return nil, e
	}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	e = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		var id string
		if e := c.QueryRowContext(ctx, "SELECT id FROM repositories WHERE id=?", repository).Scan(&id); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return false, Fail("repository-not-found", "Repository was not found")
			}
			return false, e
		}
		_, e := c.ExecContext(ctx, "INSERT OR IGNORE INTO repository_aliases VALUES(?,?)", repository, remote)
		return true, e
	})
	return map[string]string{"repositoryId": repository, "remote": remote}, e
}

func resolveProject(ctx context.Context, db *sql.DB, selector, cwd string) (Resolution, error) {
	return resolveProjectLocation(ctx, db, selector, cwd, true)
}

// Task preflight can resolve explicit and directly bound Projects without using Git.
func resolveProjectLocation(ctx context.Context, db *sql.DB, selector, cwd string, includeGit bool) (Resolution, error) {
	r := Resolution{Source: "unbound", Candidates: []Candidate{}}
	path, e := canonical(cwd)
	if e != nil {
		return r, e
	}
	r.Directory = path
	if includeGit {
		r.Git = discoverGit(ctx, path)
	}
	return resolveProjectAt(ctx, db, selector, r, includeGit)
}

func resolveProjectAt(ctx context.Context, db projectReader, selector string, r Resolution, gitDiscovered bool) (Resolution, error) {
	path := r.Directory
	e := db.QueryRowContext(ctx, "SELECT id,name,os,arch,created_at FROM device WHERE singleton=1").Scan(&r.Device.ID, &r.Device.Name, &r.Device.OS, &r.Device.Arch, &r.Device.CreatedAt)
	if e != nil {
		return r, e
	}
	if selector != "" {
		p, e := selectProject(ctx, db, selector)
		if e != nil {
			return r, e
		}
		r.Project = &p
		r.Source = "explicit"
		return r, nil
	}
	rows, e := db.QueryContext(ctx, "SELECT project_id,path FROM checkout_bindings WHERE device_id=? AND "+live("checkout_bindings.project_id"), r.Device.ID)
	if e != nil {
		return r, e
	}
	best, id := "", ""
	for rows.Next() {
		var pid, p string
		if e = rows.Scan(&pid, &p); e != nil {
			rows.Close()
			return r, e
		}
		if contains(p, path) && len(p) > len(best) {
			best, id = p, pid
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	if id != "" {
		p, e := selectProject(ctx, db, id)
		r.Project = &p
		r.Source = "path-binding"
		return r, e
	}
	if !gitDiscovered {
		r.Git = discoverGit(ctx, path)
	}
	if r.Git.CommonDir == "" {
		return resolveCandidates(ctx, db, r, nil)
	}
	// Locally explicit common-dir evidence is stronger than remote candidates.
	rows, e = db.QueryContext(ctx, `SELECT DISTINCT p.id,p.slug,p.created_at,m.repository_id,m.id,m.subdirectory FROM checkout_bindings b JOIN memberships m ON m.id=b.membership_id JOIN projects p ON p.id=m.project_id WHERE b.device_id=? AND b.git_common_dir=? AND `+live("p.id", "m.id", "m.repository_id"), r.Device.ID, r.Git.CommonDir)
	if e != nil {
		return r, e
	}
	common := []Candidate{}
	for rows.Next() {
		var c Candidate
		if e = rows.Scan(&c.Project.ID, &c.Project.Slug, &c.Project.CreatedAt, &c.RepositoryID, &c.MembershipID, &c.Subdirectory); e != nil {
			rows.Close()
			return r, e
		}
		c.Evidence = "git-common-dir"
		common = append(common, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	return resolveCandidates(ctx, db, r, common)
}

func resolveCandidates(ctx context.Context, db projectReader, r Resolution, common []Candidate) (Resolution, error) {
	if len(common) > 0 {
		depth := -1
		selected := []Candidate{}
		for _, c := range common {
			scope := filepath.Join(r.Git.CheckoutRoot, filepath.FromSlash(c.Subdirectory))
			if contains(scope, r.Directory) {
				d := len(c.Subdirectory)
				if c.Subdirectory == "." {
					d = 0
				}
				if d > depth {
					depth = d
					selected = nil
				}
				if d == depth {
					selected = append(selected, c)
				}
			}
		}
		if len(selected) > 0 {
			common = selected
			unique := map[string]Project{}
			for _, c := range selected {
				unique[c.Project.ID] = c.Project
			}
			if len(unique) == 1 {
				for _, p := range unique {
					r.Project = &p
				}
				r.Source = "git-common-dir"
				r.Candidates = common
				return r, nil
			}
		}
		r.Candidates = common
	} else {
		seen := map[string]bool{}
		for _, remote := range r.Git.remotes {
			rows, e := db.QueryContext(ctx, `SELECT p.id,p.slug,p.created_at,m.repository_id,m.id,m.subdirectory FROM repository_aliases a JOIN memberships m ON m.repository_id=a.repository_id JOIN projects p ON p.id=m.project_id WHERE a.remote=? AND `+live("p.id", "m.id", "m.repository_id"), remote)
			if e != nil {
				return r, e
			}
			for rows.Next() {
				var c Candidate
				if e = rows.Scan(&c.Project.ID, &c.Project.Slug, &c.Project.CreatedAt, &c.RepositoryID, &c.MembershipID, &c.Subdirectory); e != nil {
					rows.Close()
					return r, e
				}
				c.Evidence = "remote"
				if !seen[c.MembershipID] {
					r.Candidates = append(r.Candidates, c)
					seen[c.MembershipID] = true
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return r, e
			}
		}
	}
	sort.Slice(r.Candidates, func(i, j int) bool {
		a, b := r.Candidates[i], r.Candidates[j]
		if a.Project.Slug != b.Project.Slug {
			return a.Project.Slug < b.Project.Slug
		}
		return a.MembershipID < b.MembershipID
	})
	msg := "current directory has no project binding"
	for _, c := range r.Candidates {
		msg += "\nCandidate: " + c.Project.Slug + " (" + c.Project.ID + ") [" + c.Evidence + "]"
	}
	return r, &Error{Code: "project-unbound", Message: msg, Details: r}
}
