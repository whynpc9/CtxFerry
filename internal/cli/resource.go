package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func resourceCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) *cobra.Command {
	group := &cobra.Command{Use: "resource", Short: "Register and inspect Project services"}
	run := func(cmd *cobra.Command, args []string, fn func(app.App, string) (any, error)) error {
		return invoke(func(a app.App) (any, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return fn(a, cwd)
		})(cmd, args)
	}
	group.RunE = func(cmd *cobra.Command, _ []string) error {
		return invoke(func(app.App) (any, error) { return map[string]any{"help": cmd.UsageString()}, nil })(cmd, nil)
	}
	var spec app.ResourceSpec
	var storage string
	add := &cobra.Command{Use: "add", Aliases: []string{"register"}, Short: "Register a service and its non-secret configuration", Args: cobra.NoArgs}
	add.Flags().StringVar(&spec.Identity, "identity", "", "Service identity")
	add.Flags().StringVar(&spec.Kind, "kind", "", "Service kind")
	add.Flags().StringVar(&spec.Stage, "stage", "", "Business stage")
	add.Flags().StringVar(&spec.Purpose, "purpose", "", "Project purpose")
	add.Flags().StringVar(&storage, "storage", "", "Required: local-only or shared")
	add.Flags().StringArrayVar(&spec.Endpoints, "endpoint", nil, "Non-secret host:port (repeatable)")
	add.Flags().StringVar(&spec.Network, "network", "", "Required network label, if applicable")
	add.Flags().StringArrayVar(&spec.Platforms, "platform", nil, "Allowed os/arch pair (repeatable)")
	add.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) { return a.AddResource(ctx, *selector, cwd, storage, spec) })
	}
	list := &cobra.Command{Use: "list", Short: "List configured services for the Project", Args: cobra.NoArgs}
	list.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) { return a.ListResources(ctx, *selector, cwd) })
	}
	inspect := &cobra.Command{Use: "inspect <resource-uuid>", Short: "Read service configuration, local route and probe history", Args: cobra.ExactArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) { return a.InspectResource(ctx, *selector, cwd, args[0]) })
	}
	check := &cobra.Command{Use: "check <resource-uuid>", Short: "Connect to the resolved TCP endpoint and record reachability", Args: cobra.ExactArgs(1)}
	check.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) { return a.CheckResource(ctx, *selector, cwd, args[0]) })
	}
	var endpoint, proxy, network string
	route := &cobra.Command{Use: "route <resource-uuid>", Short: "Replace or clear this device's route facts", Args: cobra.ExactArgs(1)}
	route.Flags().StringVar(&endpoint, "endpoint", "", "Device-local service host:port")
	route.Flags().StringVar(&proxy, "proxy", "", "Device-local proxy host:port")
	route.Flags().StringVar(&network, "network", "", "Network label for this device")
	route.RunE = func(cmd *cobra.Command, args []string) error {
		return run(cmd, args, func(a app.App, cwd string) (any, error) {
			return a.SetResourceRoute(ctx, *selector, cwd, args[0], endpoint, proxy, network)
		})
	}
	group.AddCommand(add, list, inspect, check, route)
	return group
}
