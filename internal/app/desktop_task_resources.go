package app

import (
	"context"
	"database/sql"
)

type DesktopTaskResource struct {
	Slot     string
	Resource Resource
	Probe    *DesktopResourceProbe
	Latest   *Observation
}

func (a App) DesktopTaskResources(ctx context.Context, projectID, profile, taskID string) ([]DesktopTaskResource, error) {
	db, p, err := a.openDesktopProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	loc := configurationLocation{project: p}
	if err := tx.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&loc.workspace, &loc.device); err != nil {
		return nil, err
	}
	_, cfg, err := secretProfile(ctx, tx, loc, profile)
	if err != nil {
		return nil, err
	}
	task, err := loadTask(ctx, tx, p.ID, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]DesktopTaskResource, 0, len(task.ResourceSlots))
	for _, slot := range task.ResourceSlots {
		id, err := mappedResourceID(cfg, task, slot.Name)
		if err != nil {
			return nil, err
		}
		resource, err := loadMappedResource(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, DesktopTaskResource{Slot: slot.Name, Resource: resource})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range out {
		fleet, err := a.DesktopFleetView(ctx, projectID, out[i].Resource.ID)
		if err != nil {
			return nil, err
		}
		out[i].Probe = fleet.Probe
		for _, env := range fleet.Devices {
			if env.Local && len(env.Observations) > 0 {
				observation := env.Observations[0]
				out[i].Latest = &observation
				break
			}
		}
	}
	return out, nil
}
