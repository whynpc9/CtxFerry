package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

const (
	skillPinKind      = "skill-pin"
	skillGroupKind    = "skill-group"
	privateSkillLimit = BodyLimit
)

type SkillGroup struct {
	Name   string   `json:"name"`
	Skills []string `json:"skills"`
}

type SkillGroupPlan struct {
	Group SkillGroup  `json:"group"`
	Plans []SkillPlan `json:"plans"`
}

func skillRecordID(workspace, kind string, scope Scope, name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("ctxferry/"+workspace+"/"+kind+"/"+scope.Kind+"/"+scope.ID+"/"+name)).String()
}

func validPublicSkillURL(source string) bool {
	u, err := url.Parse(source)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.Path != "" && u.Path != "/" && u.RawQuery == "" && !u.ForceQuery && !strings.Contains(source, "#")
}

func resolveSkillGroup(ctx context.Context, q rowQuerier, workspace, selection string) (Scope, string, error) {
	scope := Scope{"workspace", workspace}
	switch {
	case selection == "global":
		return scope, selection, nil
	case strings.HasPrefix(selection, "capability:") && profileName(strings.TrimPrefix(selection, "capability:")):
		return scope, selection, nil
	case strings.HasPrefix(selection, "project:"):
		project, err := selectProject(ctx, q, strings.TrimPrefix(selection, "project:"))
		return Scope{"project", project.ID}, "project", err
	default:
		return scope, "", Fail("usage", "choose global, project:<project>, or capability:<name>")
	}
}

func (r remoteRecord) validateSkillRecord(workspace string) error {
	invalid := Fail("incomplete", "invalid shared Skill pin, group or scope")
	if r.WorkspaceID != workspace || r.Policy != (Policy{"shared", "full"}) {
		return invalid
	}
	workspaceScope := Scope{"workspace", workspace}
	if r.Kind == skillPinKind {
		pkg := r.SkillPin
		if !validSkillName(pkg.Name) || r.Scope != workspaceScope ||
			r.ID != skillRecordID(workspace, r.Kind, r.Scope, pkg.Name) ||
			pkg.validate(pkg.Name) != nil || (&skillPayload{files: pkg.Files}).finish() != nil ||
			validateSkillRequirements(pkg.Platforms, pkg.Dependencies) != nil {
			return invalid
		}
		// A path cannot name both a file and a directory in a projected tree.
		paths := make(map[string]bool, len(pkg.Files))
		for _, file := range pkg.Files {
			for parent := file.Path; strings.Contains(parent, "/"); {
				parent = parent[:strings.LastIndexByte(parent, '/')]
				if paths[parent] {
					return invalid
				}
			}
			paths[file.Path] = true
		}
		switch pkg.SourceKind {
		case "private":
			if pkg.SourceURL == "" && pkg.Subdir == "." && pkg.Commit == "" && pkg.Release == "" {
				return nil
			}
		case "git":
			if validPublicSkillURL(pkg.SourceURL) && skillObjectID(pkg.Commit) &&
				(pkg.Subdir == "." || validateSkillPath(pkg.Subdir)) {
				return nil
			}
		}
		return invalid
	}
	group := r.SkillGroup
	if group.Skills == nil || r.ID != skillRecordID(workspace, r.Kind, r.Scope, group.Name) {
		return invalid
	}
	switch {
	case group.Name == "project":
		if r.Scope.Kind != "project" || !validUUID(r.Scope.ID) {
			return invalid
		}
	case group.Name == "global", strings.HasPrefix(group.Name, "capability:") && profileName(strings.TrimPrefix(group.Name, "capability:")):
		if r.Scope != workspaceScope {
			return invalid
		}
	default:
		return invalid
	}
	for index, name := range group.Skills {
		if !validSkillName(name) || index > 0 && name <= group.Skills[index-1] {
			return invalid
		}
	}
	return nil
}

func skillGroupDependencies(r remoteRecord) []string {
	if r.Kind != skillGroupKind {
		return nil
	}
	scope := Scope{"workspace", r.WorkspaceID}
	ids := make([]string, 0, len(r.SkillGroup.Skills))
	for _, name := range r.SkillGroup.Skills {
		ids = append(ids, skillRecordID(r.WorkspaceID, skillPinKind, scope, name))
	}
	return ids
}

func (r remoteRecord) skillObjects() []string {
	if r.Kind != skillPinKind || r.SkillPin.SourceKind != "private" {
		return nil
	}
	paths := make([]string, 0, len(r.SkillPin.Files))
	for _, f := range r.SkillPin.Files {
		paths = append(paths, "objects/"+f.Digest)
	}
	return paths
}

func privateSkillData(pkg SkillPackage, object func(string) ([]byte, error)) (map[string][]byte, error) {
	total := 0
	data := make(map[string][]byte, len(pkg.Files))
	for _, file := range pkg.Files {
		raw, err := object("objects/" + file.Digest)
		if err != nil {
			return nil, err
		}
		if digest(raw) != file.Digest {
			return nil, Fail("incomplete", "private Skill object does not match its manifest")
		}
		total += len(raw)
		if total > privateSkillLimit {
			return nil, Fail("size-limit", "a shared private Skill may contain at most 1 MiB")
		}
		data[file.Path] = raw
	}
	name, _, _, err := skillFrontMatter(data["SKILL.md"])
	if err != nil || name != pkg.Name {
		return nil, Fail("incomplete", "private Skill does not match its manifest name")
	}
	return data, nil
}

func (a App) writeSkillCatalog(ctx context.Context, write func(*sql.Conn, string) error) error {
	release, err := a.syncOwner(ctx)
	if err != nil {
		return err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return err
	}
	defer db.Close()
	return a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		var workspace string
		if err := c.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&workspace); err != nil {
			return false, err
		}
		return true, write(c, workspace)
	})
}

func (a App) saveSharedSkill(ctx context.Context, pkg SkillPackage, payload skillPayload) error {
	if pkg.SourceKind == "private" && payload.size > privateSkillLimit {
		return Fail("size-limit", "a shared private Skill may contain at most 1 MiB")
	}
	return a.writeSkillCatalog(ctx, func(c *sql.Conn, workspace string) error {
		if pkg.SourceKind == "private" {
			for _, f := range pkg.Files {
				if _, err := c.ExecContext(ctx, "INSERT OR IGNORE INTO sync_objects VALUES(?,COALESCE(?,X''))", "objects/"+f.Digest, payload.data[f.Path]); err != nil {
					return err
				}
			}
		}
		return writeSkillRecord(ctx, c, workspace, skillPinKind, Scope{"workspace", workspace}, pkg.Name, pkg)
	})
}

func (a App) SetSkillGroup(ctx context.Context, selection string, names []string) (SkillGroup, error) {
	group := SkillGroup{Skills: append([]string{}, names...)}
	slices.Sort(group.Skills)
	group.Skills = slices.Compact(group.Skills)
	for _, name := range group.Skills {
		if !validSkillName(name) {
			return group, Fail("usage", "a valid Skill name is required")
		}
	}
	err := a.writeSkillCatalog(ctx, func(c *sql.Conn, workspace string) error {
		scope, name, err := resolveSkillGroup(ctx, c, workspace, selection)
		if err != nil {
			return err
		}
		group.Name = name
		for _, name := range group.Skills {
			id := skillRecordID(workspace, skillPinKind, Scope{"workspace", workspace}, name)
			r, err := loadSkillRecord(ctx, c, id)
			if errors.Is(err, sql.ErrNoRows) || err == nil && r.Deleted {
				return Fail("invalid-reference", fmt.Sprintf("Skill %q must have a shared pin before it can join a shared group", name))
			}
			if err != nil {
				return err
			}
		}
		return writeSkillRecord(ctx, c, workspace, skillGroupKind, scope, group.Name, group)
	})
	group.Name = selection
	return group, err
}

func loadSkillRecord(ctx context.Context, q rowQuerier, id string) (remoteRecord, error) {
	var raw []byte
	if err := q.QueryRowContext(ctx, "SELECT metadata FROM skill_records WHERE id=?", id).Scan(&raw); err != nil {
		return remoteRecord{}, err
	}
	return decodeRecord(raw)
}

func writeSkillRecord(ctx context.Context, c *sql.Conn, workspace, kind string, scope Scope, name string, payload any) error {
	id := skillRecordID(workspace, kind, scope, name)
	if err := requireNoConflict(ctx, c, id); err != nil {
		return err
	}
	prior, err := loadSkillRecord(ctx, c, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	record := publishedEnvelope{
		ID: id, Kind: kind, Scope: scope, WorkspaceID: workspace, SchemaVersion: 1,
		Revision: uuid.NewString(), OperationID: uuid.NewString(), PreviousRevision: prior.Revision,
		Policy: Policy{"shared", "full"}, Payload: payload,
	}
	record.Provenance.UpdatedAt = now()
	if err := c.QueryRowContext(ctx, "SELECT id FROM device").Scan(&record.Provenance.DeviceID); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw)+1 > MetadataLimit {
		return Fail("size-limit", "shared Skill record exceeds 256 KiB")
	}
	current, err := decodeRecord(raw)
	if err != nil {
		return err
	}
	if err := current.validateSkillRecord(workspace); err != nil {
		return err
	}
	if !prior.Deleted && bytes.Equal(current.Data, prior.Data) {
		return nil
	}
	return installSkillRecord(ctx, c, current)
}

func installSkillRecord(ctx context.Context, c *sql.Conn, r remoteRecord) error {
	raw, err := json.Marshal(r.publishedEnvelope)
	if err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "INSERT INTO skill_records VALUES(?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", r.ID, string(raw))
	return err
}

func localSkillObject(ctx context.Context, q rowQuerier, path string) ([]byte, error) {
	var b []byte
	if err := q.QueryRowContext(ctx, "SELECT data FROM sync_objects WHERE path=?", path).Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, Fail("incomplete", "private Skill object is missing")
		}
		return nil, err
	}
	return b, nil
}

// Export only typed Skill records of one kind and the private objects their manifests reference.
func (x *snapshotExport) exportSkillRecords(kind string) error {
	rows, err := x.conn.QueryContext(x.ctx, "SELECT id,metadata FROM skill_records ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	var records []remoteRecord
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		r, err := decodeRecord(raw)
		if err != nil {
			return err
		}
		if len(raw)+1 > MetadataLimit || r.ID != id || !validUUID(r.ID) ||
			!slices.Contains(registeredKinds("skill_records"), r.Kind) || r.Scope.Kind == "project" && !x.projects[r.Scope.ID] {
			return Fail("invalid-reference", "invalid shared Skill record identity or Project dependency")
		}
		if r.Kind != kind {
			continue
		}
		if x.files[recordPath(r.ID)] != nil {
			return Fail("invalid-reference", "invalid shared Skill record identity or Project dependency")
		}
		if err := r.validateSkillRecord(x.conf.WorkspaceID); err != nil {
			return err
		}
		x.files[recordPath(r.ID)] = append(raw, '\n')
		x.ops = append(x.ops, r.OperationID)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range records {
		for _, path := range r.skillObjects() {
			if _, exists := x.files[path]; exists {
				continue
			}
			b, err := localSkillObject(x.ctx, x.conn, path)
			if err != nil {
				return err
			}
			x.files[path] = b
		}
	}
	return nil
}
