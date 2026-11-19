package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func doctorCommand(ctx context.Context, invoke func(func(app.App) (any, error)) func(*cobra.Command, []string) error, selector *string) *cobra.Command {
	return &cobra.Command{
		Use: "doctor", Short: "Report local device capabilities and sync evidence", Args: cobra.NoArgs,
		RunE: invoke(func(a app.App) (any, error) {
			cwd, _ := os.Getwd()
			return a.Doctor(ctx, *selector, cwd), nil
		}),
	}
}

func renderDoctor(w io.Writer, r app.DoctorReport) error {
	line := func(label string, check app.DoctorCheck) error {
		_, err := fmt.Fprintf(w, "%s: %s (%s)\n", label, check.Status, check.Evidence)
		return err
	}
	for _, item := range []struct {
		label string
		check app.DoctorCheck
	}{
		{"CLI", r.CLI}, {"Catalog", r.Catalog}, {"Directory binding", r.DirectoryBinding.DoctorCheck},
		{"Path resolution", r.PathResolution.DoctorCheck}, {"Working directory", r.PathResolution.WorkingDirectory},
		{"Git authentication", r.GitAuth.DoctorCheck}, {"Provider profiles", r.ProviderAuth.DoctorCheck},
		{"Last sync", r.LastSync.DoctorCheck}, {"Secure credential storage", r.CredentialStore}, {"Background sync", r.BackgroundSync},
		{"Resources", r.Resources.DoctorCheck},
	} {
		if err := line(item.label, item.check); err != nil {
			return err
		}
	}
	for _, hint := range r.GitAuth.Mechanisms {
		if _, err := fmt.Fprintln(w, "Git auth configuration:", hint); err != nil {
			return err
		}
	}
	for _, p := range r.ProviderAuth.Profiles {
		label := fmt.Sprintf("%s/%s (%s; headless %s)", p.Profile.Provider, p.Profile.Name, p.Profile.Method, p.Headless)
		if err := line(label, p.DoctorCheck); err != nil {
			return err
		}
	}
	if r.LastSync.LastSyncAt != nil {
		if _, err := fmt.Fprintln(w, "Last successful sync at:", r.LastSync.LastSyncAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if r.LastSync.Pending != nil {
		if _, err := fmt.Fprintf(w, "Sync state: %s (%d queued)\n", r.LastSync.State, *r.LastSync.Pending); err != nil {
			return err
		}
	}
	for _, d := range r.Dependencies {
		if err := line(d.Name+" ("+d.Use+")", d.DoctorCheck); err != nil {
			return err
		}
	}
	for _, resource := range r.Resources.Items {
		if _, err := fmt.Fprintf(w, "Resource %s: route %s, recorded TCP reachability %s, service identity %s\n",
			resource.ID, resource.Route, resource.TCPReachability, resource.ServiceIdentity); err != nil {
			return err
		}
	}
	return nil
}
