package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

const secretBindHelp = `Map a Task's credential requirement to a SecretBinding in a Profile.

Reuse an existing binding with --binding <uuid>, or describe a new one. For a
new binding, --alias defaults to the requirement name, --path to / and --key to
the requirement's delivery variable. --like <alias-or-uuid> copies the provider,
provider profile, method, backend project, environment and path from a binding
the Project can see; flags given explicitly override the copied values.

Without --profile the checkout's selected Profile is used, then the Project's
Profile named default. When neither exists and the Project has no Profiles,
bind creates the default Profile.

Examples:
  ctxferry secret bind agent openai --provider infisical --provider-profile default \
    --method universal-auth --backend-project <id> --environment dev
  ctxferry secret bind agent anthropic --like openai`

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
	status.Flags().StringArrayVar(&statusProfiles, "profile", nil, "Profile (defaults to this checkout's selected Profile, then the Profile named default)")
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

	bind := &cobra.Command{Use: "bind <task> <requirement>", Short: "Create and map a SecretBinding or reuse one by UUID", Long: secretBindHelp, Args: cobra.ExactArgs(2)}
	var change app.SecretBindingChange
	var bindProfiles, bindingIDs []string
	flags := bind.Flags()
	flags.StringArrayVar(&bindProfiles, "profile", nil, "Profile (defaults to this checkout's selected Profile, then the Profile named default)")
	flags.StringArrayVar(&bindingIDs, "binding", nil, "Existing shared SecretBinding UUID")
	flags.StringVar(&change.Like, "like", "", "Copy provider and backend namespace from this binding (alias or UUID)")
	flags.StringVar(&change.Alias, "alias", "", "New binding's logical alias (defaults to the requirement name)")
	flags.StringVar(&change.ProviderProfile.Provider, "provider", "", "Provider adapter name")
	flags.StringVar(&change.ProviderProfile.Name, "provider-profile", "", "Provider profile name")
	flags.StringVar(&change.ProviderProfile.Method, "method", "", "Provider authentication method")
	flags.StringVar(&change.Locator.EntryID, "entry-id", "", "Exact stable backend entry ID")
	flags.StringVar(&change.Locator.ProjectID, "backend-project", "", "Backend project ID for a named entry")
	flags.StringVar(&change.Locator.Environment, "environment", "", "Backend environment for a named entry")
	flags.StringVar(&change.Locator.Path, "path", "", "Backend path for a named entry (defaults to /)")
	flags.StringVar(&change.Locator.Key, "key", "", "Exact backend key for a named entry (defaults to the requirement's variable)")
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
				for _, name := range []string{"like", "alias", "provider", "provider-profile", "method", "entry-id", "backend-project", "environment", "path", "key"} {
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
	unmap := &cobra.Command{Use: "unmap <task> <requirement>", Short: "Remove an explicit credential mapping from a Profile", Args: cobra.ExactArgs(2)}
	var unmapProfiles []string
	unmap.Flags().StringArrayVar(&unmapProfiles, "profile", nil, "Profile (defaults to this checkout's selected Profile, then the Profile named default)")
	unmap.RunE = func(cmd *cobra.Command, args []string) error {
		return invoke(func(a app.App) (any, error) {
			profile, err := explicitProfile(unmapProfiles)
			if err != nil {
				return nil, err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			target := app.SecretTarget{Profile: profile, Task: args[0], Requirement: args[1]}
			return a.UnmapSecret(ctx, *project, cwd, target)
		})(cmd, args)
	}
	secret.AddCommand(list, status, bind, unmap)

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
