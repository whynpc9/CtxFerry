package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type ConfigurationChange struct {
	Layer   string
	Profile string
	Field   string
	Value   json.RawMessage // nil removes the exact field
	Create  bool
}

type ConfigurationReceipt struct {
	Layer   string `json:"layer"`
	Storage string `json:"storage"`
	Profile string `json:"profile,omitempty"`
	ID      string `json:"id,omitempty"`
	Field   string `json:"field,omitempty"`
}

func (a App) ChangeConfiguration(ctx context.Context, selector, cwd string, change ConfigurationChange) (ConfigurationReceipt, error) {
	out := ConfigurationReceipt{Layer: change.Layer, Profile: change.Profile, Field: change.Field, Storage: "shared"}
	local := change.Layer == layerDevice || change.Layer == layerCheckout
	if local {
		out.Storage = "local-only"
	}
	switch change.Layer {
	case layerGlobal, layerProject, layerDevice, layerCheckout:
		if change.Profile != "" || change.Create {
			return out, Fail("usage", "Profile name applies only to the profile layer")
		}
	case layerProfile:
		if !profileName(change.Profile) {
			return out, Fail("invalid-profile", "the profile layer requires a valid explicit Profile name")
		}
	default:
		return out, Fail("usage", "choose --layer global, project, profile, device or checkout")
	}
	if !change.Create {
		raw := change.Value
		if raw == nil {
			// Validate that unset names an exact ordinary field as well.
			switch change.Field {
			case "parallelism", "timeoutSeconds":
				raw = json.RawMessage("1")
			case "workingDirectory":
				raw = json.RawMessage(`"."`)
			}
		}
		if _, err := configValue(change.Field, raw, local); err != nil {
			return out, err
		}
	}
	if change.Layer == layerGlobal && selector != "" {
		return out, Fail("usage", "global configuration applies to the workspace")
	}
	if !local {
		release, err := a.syncOwner(ctx)
		if err != nil {
			return out, err
		}
		defer release()
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	var loc configurationLocation
	if change.Layer == layerGlobal {
		err = db.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&loc.workspace)
	} else {
		loc, err = configLocation(ctx, db, selector, cwd)
	}
	if err != nil {
		return out, err
	}
	if local {
		checkout := ""
		if change.Layer == layerCheckout {
			checkout, err = requireCheckout(loc)
			if err != nil {
				return out, err
			}
		}
		err = store.Write(ctx, db, func(c *sql.Conn) error {
			v, exists, err := readConfiguration(c.QueryRowContext(ctx, `SELECT payload FROM local_configurations
				WHERE device_id=? AND project_id=? AND checkout=?`, loc.device, loc.project.ID, checkout), true)
			if err != nil || (change.Value == nil && !exists) {
				return err
			}
			if !change.apply(&v) {
				return nil
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			_, err = c.ExecContext(ctx, `INSERT INTO local_configurations VALUES(?,?,?,?)
				ON CONFLICT(device_id,project_id,checkout) DO UPDATE SET payload=excluded.payload`, loc.device, loc.project.ID, checkout, string(raw))
			return err
		})
		return out, err
	}

	if err := a.prepareBaseline(ctx, db); err != nil {
		return out, err
	}
	scope := Scope{"workspace", loc.workspace}
	if change.Layer != layerGlobal {
		scope = Scope{"project", loc.project.ID}
	}
	out.ID = configID(loc.workspace, scope, change.Profile)
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if err := requireNoConflict(ctx, c, out.ID); err != nil {
			return err
		}
		v, exists, err := readConfiguration(c.QueryRowContext(ctx, "SELECT payload FROM configurations WHERE id=?", out.ID), false)
		if err != nil {
			return err
		}
		if change.Create && exists {
			return Fail("profile-exists", "Profile already exists in this Project")
		}
		if change.Layer == layerProfile && !change.Create && !exists {
			return Fail("profile-not-found", "create the Profile before editing its configuration")
		}
		if !change.apply(&v) {
			return nil
		}
		if err := saveConfiguration(ctx, c, out.ID, scope, v); err != nil {
			return err
		}
		return queueChanges(ctx, c)
	})
	return out, err
}

func (change ConfigurationChange) apply(v *configuration) bool {
	if change.Create {
		v.Profile = change.Profile
	} else if change.Value == nil {
		if _, exists := v.Values[change.Field]; !exists {
			return false
		}
		delete(v.Values, change.Field)
	} else {
		value, _ := configValue(change.Field, change.Value, change.Layer == layerDevice || change.Layer == layerCheckout)
		raw, _ := json.Marshal(value)
		if string(raw) == string(v.Values[change.Field]) {
			return false
		}
		v.Values[change.Field] = raw
	}
	v.UpdatedAt, v.Revision = now(), uuid.NewString()
	return true
}

func saveConfiguration(ctx context.Context, c *sql.Conn, id string, scope Scope, v configuration) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var projectID any
	if scope.Kind == "project" {
		projectID = scope.ID
	}
	_, err = c.ExecContext(ctx, `INSERT INTO configurations VALUES(?,?,?)
		ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, id, projectID, string(raw))
	return err
}

type ProfileSelection struct {
	Checkout CurrentCheckout  `json:"checkout"`
	Profile  *SelectedProfile `json:"profile"`
}

func (a App) SelectProfile(ctx context.Context, selector, cwd, name string) (ProfileSelection, error) {
	out := ProfileSelection{}
	if name != "" && !profileName(name) {
		return out, Fail("invalid-profile", "Profile name must be a slug of up to 63 characters")
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	loc, err := configLocation(ctx, db, selector, cwd)
	if err != nil {
		return out, err
	}
	path, err := requireCheckout(loc)
	if err != nil {
		return out, err
	}
	out.Checkout = *loc.checkout
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if name == "" {
			_, err := c.ExecContext(ctx, "DELETE FROM profile_selections WHERE device_id=? AND project_id=? AND checkout=?", loc.device, loc.project.ID, path)
			return err
		}
		id := configID(loc.workspace, Scope{"project", loc.project.ID}, name)
		var exists int
		err := c.QueryRowContext(ctx, "SELECT 1 FROM configurations WHERE id=?", id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return Fail("profile-not-found", "Profile was not found in this Project")
		}
		if err != nil {
			return err
		}
		_, err = c.ExecContext(ctx, `INSERT INTO profile_selections VALUES(?,?,?,?,?)
			ON CONFLICT(device_id,project_id,checkout) DO UPDATE SET profile=excluded.profile,selected_at=excluded.selected_at
			WHERE profile_selections.profile<>excluded.profile`, loc.device, loc.project.ID, path, name, now())
		if err != nil {
			return err
		}
		out.Profile, err = selectedProfile(ctx, c, loc)
		return err
	})
	return out, err
}
