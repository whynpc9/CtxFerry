package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
)

type DesktopFleet struct {
	Resources []Resource
	Selected  Resource
	Route     ResourceRoute
	Probe     *DesktopResourceProbe
	Devices   []DesktopEnvironment
}

type DesktopEnvironment struct {
	Device       Device
	Kind         string
	Local        bool
	Observations []Observation
}

type DesktopResourceProbe struct {
	ProjectID, ResourceID, DeviceID string
	Scope                           ObservationScope
}

// DesktopFleetView returns the route on this device and observations tagged
// with the device that produced them. Shared provenance alone identifies a
// device ID, but carries no hostname, login evidence or remote route.
func (a App) DesktopFleetView(ctx context.Context, project, resourceID string) (DesktopFleet, error) {
	var out DesktopFleet
	db, p, err := a.openProjectReadOnly(ctx, project)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	list, err := listProjectResources(ctx, tx, p)
	if err != nil {
		return out, err
	}
	out.Resources = list.Resources
	slices.SortFunc(out.Resources, func(a, b Resource) int {
		if a.Identity != b.Identity {
			return strings.Compare(a.Identity, b.Identity)
		}
		return strings.Compare(a.ID, b.ID)
	})
	var local DesktopEnvironment
	d := &local.Device
	if err := tx.QueryRowContext(ctx, "SELECT id,name,os,arch,created_at FROM device WHERE singleton=1").
		Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.CreatedAt); err != nil {
		return out, err
	}
	local.Local, local.Kind = true, localDesktopEnvironment()
	out.Devices = []DesktopEnvironment{local}
	if resourceID == "" && len(out.Resources) > 0 {
		resourceID = out.Resources[0].ID
	}
	if i := slices.IndexFunc(out.Resources, func(r Resource) bool { return r.ID == resourceID }); i >= 0 {
		detail, err := resourceDetail(ctx, tx, p.ID, "", resourceID)
		if err != nil {
			return out, err
		}
		out.Selected, out.Route = detail.Resource, detail.Route
		if detail.Route.Status == "resolved" {
			out.Probe = &DesktopResourceProbe{p.ID, resourceID, local.Device.ID, resourceProbeScope(detail)}
		}
	}
	devices, err := desktopEnvironments(ctx, tx, p.ID, out.Selected.ID, local)
	if err != nil {
		return out, err
	}
	out.Devices = devices
	return out, tx.Commit()
}

func desktopEnvironments(ctx context.Context, tx *sql.Tx, project, resource string, local DesktopEnvironment) ([]DesktopEnvironment, error) {
	// Resource observations are device-local in the current sync schema.
	// Retain imported or historical evidence if this database has it.
	rows, err := tx.QueryContext(ctx, `WITH known(id) AS (
		SELECT json_extract(metadata,'$.provenance.deviceId') FROM records WHERE project_id=?
		AND coalesce(json_extract(metadata,'$.deleted'),0)=0 AND `+live("records.id")+`
		UNION SELECT device_id FROM resource_observations WHERE resource_id IN
			(SELECT id FROM records WHERE project_id=? AND kind='resource')
	)
	SELECT id, (SELECT data FROM resource_observations o JOIN records r ON o.resource_id=r.id
		WHERE o.device_id=known.id AND r.project_id=? ORDER BY o.seq DESC LIMIT 1)
	FROM known WHERE id IS NOT NULL AND id<>? ORDER BY id`, project, project, project, local.Device.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	environments := []DesktopEnvironment{local}
	byID := map[string]int{local.Device.ID: 0}
	for rows.Next() {
		var id string
		var raw sql.NullString
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if !validUUID(id) {
			continue
		}
		env := DesktopEnvironment{Device: Device{ID: id}}
		if raw.Valid {
			var observation Observation
			if err := json.Unmarshal([]byte(raw.String), &observation); err != nil {
				return nil, err
			}
			if observation.DeviceID == id {
				env.Device.OS, env.Device.Arch = observation.Scope.OS, observation.Scope.Arch
				if env.Device.OS == "linux" && observation.EnvironmentKind == "WSL" {
					env.Kind = observation.EnvironmentKind
				}
			}
		}
		byID[id] = len(environments)
		environments = append(environments, env)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if resource == "" {
		return environments, nil
	}
	rows, err = tx.QueryContext(ctx, `SELECT device_id,data FROM (
		SELECT device_id,data,seq,row_number() OVER (PARTITION BY device_id ORDER BY seq DESC) AS rank
		FROM resource_observations WHERE resource_id=?
	) WHERE rank<=12 ORDER BY seq DESC`, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var observation Observation
		if err := json.Unmarshal(raw, &observation); err != nil {
			return nil, err
		}
		if i, exists := byID[id]; exists && observation.DeviceID == id && observation.ResourceID == resource {
			environments[i].Observations = append(environments[i].Observations, observation)
		}
	}
	return environments, rows.Err()
}

func localDesktopEnvironment() string {
	if runtime.GOOS == "linux" {
		release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
		if strings.Contains(strings.ToLower(string(release)), "microsoft") {
			return "WSL"
		}
	}
	return ""
}

func (a App) ProbeDesktopResource(ctx context.Context, expected DesktopResourceProbe) (Observation, error) {
	return a.checkResource(ctx, expected.ProjectID, a.DataDir, expected.ResourceID, &expected)
}
