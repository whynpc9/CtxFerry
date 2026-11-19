package cli

import (
	"context"
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func configurationCommands(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) (*cobra.Command, *cobra.Command) {
	config := &cobra.Command{Use: "config", Short: "Edit ordinary configuration at a specific layer"}
	profile := &cobra.Command{Use: "profile", Short: "Create a Profile or select one for the current checkout"}
	cwdCall := func(fn func(app.App, string) (any, error)) func(*cobra.Command, []string) error {
		return invoke(func(a app.App) (any, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return fn(a, cwd)
		})
	}
	for _, action := range []string{"set", "unset"} {
		var layers []string
		var names []string
		cmd := &cobra.Command{Use: "unset <field>", Short: "Remove one field from the specified layer", Args: cobra.ExactArgs(1)}
		if action == "set" {
			cmd.Use = "set <field> <json-value>"
			cmd.Short = "Set one ordinary field at the specified layer"
			cmd.Args = cobra.ExactArgs(2)
		}
		cmd.Flags().StringArrayVar(&layers, "layer", nil, "Required layer (global, project, profile, device, checkout)")
		cmd.Flags().StringArrayVar(&names, "profile", nil, "Profile name for the profile layer")
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return cwdCall(func(a app.App, cwd string) (any, error) {
				name, err := explicitProfile(names)
				if err != nil {
					return nil, err
				}
				if len(layers) != 1 {
					return nil, app.Fail("usage", "choose exactly one --layer")
				}
				change := app.ConfigurationChange{Layer: layers[0], Profile: name, Field: args[0]}
				if action == "set" {
					change.Value = json.RawMessage(args[1])
				}
				return a.ChangeConfiguration(ctx, *selector, cwd, change)
			})(cmd, args)
		}
		config.AddCommand(cmd)
	}
	for _, action := range []string{"create", "select", "clear"} {
		cmd := &cobra.Command{Use: action + " <name>", Args: cobra.ExactArgs(1)}
		if action == "clear" {
			cmd.Use = "clear"
			cmd.Args = cobra.NoArgs
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return cwdCall(func(a app.App, cwd string) (any, error) {
				if action == "clear" {
					return a.SelectProfile(ctx, *selector, cwd, "")
				}
				if args[0] == "" {
					return nil, app.Fail("invalid-profile", "Profile name must not be empty")
				}
				if action == "select" {
					return a.SelectProfile(ctx, *selector, cwd, args[0])
				}
				return a.ChangeConfiguration(ctx, *selector, cwd, app.ConfigurationChange{Layer: "profile", Profile: args[0], Create: true})
			})(cmd, args)
		}
		profile.AddCommand(cmd)
	}
	mapResource := &cobra.Command{Use: "map-resource <profile> <task> <slot> <resource-uuid>", Short: "Set an explicit Profile mapping for a Task's Resource slot", Args: cobra.ExactArgs(4)}
	mapResource.RunE = func(cmd *cobra.Command, args []string) error {
		return cwdCall(func(a app.App, cwd string) (any, error) {
			return a.MapTaskResource(ctx, *selector, cwd, args[0], args[1], args[2], args[3])
		})(cmd, args)
	}
	profile.AddCommand(mapResource)
	return config, profile
}

func explicitProfile(names []string) (string, error) {
	if len(names) > 1 {
		return "", app.Fail("ambiguous-profile", "choose a single explicit Profile")
	}
	if len(names) == 1 {
		if names[0] == "" {
			return "", app.Fail("invalid-profile", "Profile name must not be empty")
		}
		return names[0], nil
	}
	return "", nil
}
