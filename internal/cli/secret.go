package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

func secretCommands(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, project *string) (*cobra.Command, *cobra.Command) {
	secret := &cobra.Command{Use: "secret", Short: "Manage credential references and check availability"}
	list := &cobra.Command{Use: "list", Short: "List the Project's SecretBinding metadata without contacting a provider", Args: cobra.NoArgs}
	list.RunE = invoke(func(a app.App) (any, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return a.ListSecrets(ctx, *project, cwd)
	})

	status := &cobra.Command{Use: "status <task> <requirement>", Short: "Check one explicitly mapped credential on this device", Args: cobra.ExactArgs(2)}
	var statusProfiles []string
	status.Flags().StringArrayVar(&statusProfiles, "profile", nil, "Profile (defaults to this checkout's selected Profile)")
	status.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			profile, err := explicitProfile(statusProfiles)
			if err != nil {
				return nil, err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			target := app.SecretTarget{Profile: profile, Task: args[0], Requirement: args[1]}
			return a.StatusSecret(ctx, *project, cwd, target)
		})(cmd, args)
	}

	bind := &cobra.Command{Use: "bind <task> <requirement>", Short: "Create and map a SecretBinding or reuse one by UUID", Args: cobra.ExactArgs(2)}
	var change app.SecretBindingChange
	var bindProfiles, bindingIDs []string
	flags := bind.Flags()
	flags.StringArrayVar(&bindProfiles, "profile", nil, "Profile (defaults to this checkout's selected Profile)")
	flags.StringArrayVar(&bindingIDs, "binding", nil, "Existing shared SecretBinding UUID")
	flags.StringVar(&change.Alias, "alias", "", "New binding's logical alias")
	flags.StringVar(&change.ProviderProfile.Provider, "provider", "", "Provider adapter name")
	flags.StringVar(&change.ProviderProfile.Name, "provider-profile", "", "Provider profile name")
	flags.StringVar(&change.ProviderProfile.Method, "method", "", "Provider authentication method")
	flags.StringVar(&change.Locator.EntryID, "entry-id", "", "Exact stable backend entry ID")
	flags.StringVar(&change.Locator.ProjectID, "backend-project", "", "Backend project ID for a named entry")
	flags.StringVar(&change.Locator.Environment, "environment", "", "Backend environment for a named entry")
	flags.StringVar(&change.Locator.Path, "path", "", "Backend path for a named entry")
	flags.StringVar(&change.Locator.Key, "key", "", "Exact backend key for a named entry")
	bind.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			profile, err := explicitProfile(bindProfiles)
			if err != nil {
				return nil, err
			}
			change.BindingID, err = explicitProfile(bindingIDs)
			if err != nil {
				return nil, app.Fail("usage", "pass exactly one nonempty --binding UUID when reusing a binding")
			}
			if change.BindingID != "" {
				for _, name := range []string{"alias", "provider", "provider-profile", "method", "entry-id", "backend-project", "environment", "path", "key"} {
					if flags.Changed(name) {
						return nil, app.Fail("usage", "pass either --binding or new binding metadata")
					}
				}
			}
			change.SecretTarget = app.SecretTarget{Profile: profile, Task: args[0], Requirement: args[1]}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return a.BindSecret(ctx, *project, cwd, change)
		})(cmd, args)
	}
	secret.AddCommand(list, status, bind)

	providers := &cobra.Command{Use: "provider", Short: "Manage device-local provider authentication"}
	var p provider.Profile
	login := &cobra.Command{Use: "login <provider>", Short: "Authenticate a provider profile on this device", Args: cobra.ExactArgs(1)}
	login.Flags().StringVar(&p.Name, "provider-profile", "", "Provider profile name")
	login.Flags().StringVar(&p.Method, "method", "", "Provider authentication method")
	login.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			if *project != "" {
				return nil, app.Fail("usage", "provider login applies to this workspace and device")
			}
			p.Provider = args[0]
			return a.LoginProvider(ctx, p)
		})(cmd, args)
	}
	providers.AddCommand(login)
	for _, command := range []*cobra.Command{bind, login} {
		command.SetFlagErrorFunc(func(*cobra.Command, error) error {
			return app.Fail("usage", "invalid flags; see command help for supported metadata fields")
		})
	}
	return secret, providers
}
