package app

import (
	"context"
	"database/sql"
)

func validConfigurationScope(id, workspace string, scope Scope, profile string) bool {
	switch scope.Kind {
	case "workspace":
		return scope.ID == workspace && profile == "" && id == configID(workspace, scope, profile)
	case "project":
		return validUUID(scope.ID) && id == configID(workspace, scope, profile)
	default:
		return false
	}
}

type configurationExport struct {
	id    string
	scope Scope
	value configuration
}

func exportConfigurations(ctx context.Context, c *sql.Conn, workspace string, projects map[string]bool) ([]configurationExport, error) {
	rows, err := c.QueryContext(ctx, "SELECT id,project_id,payload FROM configurations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []configurationExport
	for rows.Next() {
		var id, raw string
		var project sql.NullString
		if err := rows.Scan(&id, &project, &raw); err != nil {
			return nil, err
		}
		value, err := decodeConfiguration([]byte(raw), false)
		if err != nil {
			return nil, err
		}
		scope := Scope{"workspace", workspace}
		if project.Valid {
			scope = Scope{"project", project.String}
			if !projects[scope.ID] {
				return nil, Fail("invalid-reference", "shared configuration is missing its Project")
			}
		}
		if !validConfigurationScope(id, workspace, scope, value.Profile) {
			return nil, Fail("invalid-reference", "shared configuration has an invalid identity or scope")
		}
		configs = append(configs, configurationExport{id, scope, value})
	}
	return configs, rows.Err()
}
