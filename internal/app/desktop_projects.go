package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type DesktopProject struct {
	Project
	Subscribed bool
	Checkouts  []Binding
}

// ReadDesktopProjects lists the local catalog, including unsubscribed Projects
// and Projects which have no Checkout on this device.
func (a App) ReadDesktopProjects(ctx context.Context) ([]DesktopProject, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT p.id,p.slug,p.created_at,coalesce(s.subscribed,1),
		coalesce(b.id,''),coalesce(b.path,''),coalesce(b.created_at,''),coalesce(b.device_id,''),
		coalesce(m.id,''),coalesce(m.repository_id,'')
		FROM projects p LEFT JOIN project_subscriptions s ON s.project_id=p.id
		LEFT JOIN checkout_bindings b ON b.project_id=p.id AND b.device_id=(SELECT id FROM device WHERE singleton=1)
		LEFT JOIN memberships m ON m.id=b.membership_id
		WHERE `+live("p.id")+`
		ORDER BY p.slug,p.id,b.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []DesktopProject
	for rows.Next() {
		var p DesktopProject
		var b Binding
		if err := rows.Scan(&p.ID, &p.Slug, &p.CreatedAt, &p.Subscribed, &b.ID, &b.Path, &b.CreatedAt,
			&b.DeviceID, &b.MembershipID, &b.RepositoryID); err != nil {
			return nil, err
		}
		if len(projects) == 0 || projects[len(projects)-1].ID != p.ID {
			projects = append(projects, p)
		}
		if b.ID != "" {
			i := len(projects) - 1
			b.ProjectID = p.ID
			projects[i].Checkouts = append(projects[i].Checkouts, b)
		}
	}
	return projects, rows.Err()
}

// ReadProjectSkills reads the project's shared selection without generating an
// install plan, querying a source, or inspecting another project's selection.
func (a App) ReadProjectSkills(ctx context.Context, projectID string) (SkillGroup, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return SkillGroup{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SkillGroup{}, err
	}
	defer tx.Rollback()
	loc, err := projectConfigurationLocation(ctx, tx, ProjectConfigurationTarget{ProjectID: projectID})
	if err != nil {
		return SkillGroup{}, err
	}
	scope := Scope{"project", loc.project.ID}
	id := skillRecordID(loc.workspace, skillGroupKind, scope, "project")
	if err := requireNoConflict(ctx, tx, id); err != nil {
		return SkillGroup{}, err
	}
	r, err := loadSkillRecord(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && r.Deleted {
		return SkillGroup{}, nil
	}
	if err != nil {
		return SkillGroup{}, err
	}
	if err := r.validateSkillRecord(loc.workspace); err != nil {
		return SkillGroup{}, err
	}
	return r.SkillGroup, nil
}
