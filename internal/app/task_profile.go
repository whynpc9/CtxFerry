package app

import (
	"context"
	"database/sql"
	"slices"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type ResourceMapping struct {
	TaskID     string `json:"taskId"`
	Slot       string `json:"slot"`
	ResourceID string `json:"resourceId"`
}

func compatibleResource(task TaskSpec, slot string, resource ResourceSpec) bool {
	need, ok := task.slot(slot)
	return ok && need.Kind == resource.Kind && need.Stage == resource.Stage
}

func (a App) MapTaskResource(ctx context.Context, selector, cwd, profile, taskName, slot, resourceID string) (ResourceMapping, error) {
	out := ResourceMapping{Slot: slot, ResourceID: resourceID}
	if !profileName(profile) {
		return out, Fail("invalid-profile", "specify an existing Profile name")
	}
	if !validUUID(resourceID) {
		return out, Fail("invalid-reference", "specify the Resource UUID")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	if err := a.prepareBaseline(ctx, db); err != nil {
		return out, err
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		var workspace string
		if err := c.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&workspace); err != nil {
			return err
		}
		scope := Scope{"project", project.ID}
		id := configID(workspace, scope, profile)
		if err := requireNoConflict(ctx, c, id); err != nil {
			return err
		}
		cfg, exists, err := readConfiguration(c.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", id), false)
		if err != nil {
			return err
		}
		if !exists {
			return Fail("profile-not-found", "Profile was not found in this Project")
		}
		task, err := loadTask(ctx, c, project.ID, taskName)
		if err != nil {
			return err
		}
		resource, err := loadResource(ctx, c, project.ID, resourceID)
		if err != nil {
			return err
		}
		for _, target := range []string{task.ID, resource.ID} {
			if err := requireNoConflict(ctx, c, target); err != nil {
				return err
			}
		}
		if task.Policy.Storage != "shared" || resource.Policy.Storage != "shared" {
			return Fail("invalid-reference", "a shared Profile mapping requires a shared Task and Resource")
		}
		if !compatibleResource(task.TaskSpec, slot, resource.ResourceSpec) {
			return Fail("incompatible", "the Task slot must exist and match the Resource kind and stage")
		}
		out.TaskID = task.ID
		i := slices.IndexFunc(cfg.ResourceMappings, func(m ResourceMapping) bool { return m.TaskID == task.ID && m.Slot == slot })
		if i < 0 {
			cfg.ResourceMappings = append(cfg.ResourceMappings, out)
		} else if cfg.ResourceMappings[i] == out {
			return nil
		} else {
			cfg.ResourceMappings[i] = out
		}
		if len(cfg.ResourceMappings) > 512 {
			return Fail("size-limit", "a Profile supports at most 512 Resource mappings")
		}
		cfg.UpdatedAt, cfg.Revision = now(), uuid.NewString()
		if err := saveConfiguration(ctx, c, id, scope, cfg); err != nil {
			return err
		}
		return queueChanges(ctx, c)
	})
	return out, err
}

// Check the relationships as well as the existence of live dependencies.
// The planner uses these IDs to identify pending operations that broke them.
func invalidTaskDependencies(r remoteRecord, records map[string]remoteRecord) []string {
	var invalid []string
	switch r.Kind {
	case "task":
		m := records[r.Task.MembershipID]
		if m.Kind != "membership" || m.Deleted || m.Scope != r.Scope || !taskPath(m.Value.Subdirectory) {
			invalid = append(invalid, r.Task.MembershipID)
		}
	case "configuration":
		for _, mapping := range r.Config.ResourceMappings {
			task, resource := records[mapping.TaskID], records[mapping.ResourceID]
			if task.Kind != "task" || task.Deleted || task.Scope != r.Scope {
				invalid = append(invalid, mapping.TaskID)
				continue
			}
			if _, exists := task.Task.slot(mapping.Slot); !exists {
				invalid = append(invalid, mapping.TaskID)
				continue
			}
			if resource.Kind != "resource" || resource.Deleted || resource.Scope != r.Scope {
				invalid = append(invalid, mapping.ResourceID)
				continue
			}
			if !compatibleResource(task.Task, mapping.Slot, resource.Resource) {
				invalid = append(invalid, mapping.TaskID, mapping.ResourceID)
			}
		}
	}
	return invalid
}
