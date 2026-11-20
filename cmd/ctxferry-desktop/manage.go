package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type desktopProjects struct {
	items   []app.Project
	loading bool
	ready   bool
	err     error
}

func desktopWork[T any](d *desktop, run func(context.Context) (T, error), done func(T, error)) {
	if d.window == nil {
		return
	}
	generation := d.managementGeneration
	runDesktop(d, run, func(result T, err error) {
		if d.managementGeneration == generation {
			done(result, err)
		}
	})
}

func (d *desktop) refreshManagement() {
	if d.projects.loading || d.window == nil {
		return
	}
	d.projects.loading = true
	desktopWork(d, d.core.DesktopProjects, func(result app.ListResult, err error) {
		d.projects = desktopProjects{items: result.Projects, ready: true, err: err}
		if err != nil {
			return
		}
		if d.route.Page == pageSecrets && !d.secrets.wait {
			d.loadCredentials(d.validProject(valueOr(d.route.ProjectID, d.secrets.project)))
		} else if d.route.Page == pageFleet && !d.fleet.wait {
			project, resource := d.fleetTarget()
			d.loadFleet(project, resource)
		}
	})
}

func (d *desktop) validProject(id string) string {
	for _, project := range d.projects.items {
		if project.ID == id {
			return id
		}
	}
	if len(d.projects.items) > 0 {
		return d.projects.items[0].ID
	}
	return ""
}

func (d *desktop) managementReady(c *ui.Context) bool {
	if !d.projects.ready && !d.projects.loading {
		d.refreshManagement()
	}
	if !d.projects.ready {
		muted(c, "Reading subscribed Projects…")
		return false
	}
	if d.projects.err != nil {
		banner(c, toneBlocked, "Projects could not be read", "Refresh the local view before selecting a Catalog scope.", nil)
		return false
	}
	if len(d.projects.items) == 0 {
		card(c).Children(func() {
			cardHeader(c, icFolder, "No subscribed Projects", "Open Projects to subscribe or bind one. Device-wide Skills remain available from Catalog.", nil)
			linkButton(c, "Open Projects", icArrowR).AlignSelf(ui.Start).OnClick(func() { d.navigateRoot(pageProjects) })
		})
		return false
	}
	return true
}

type desktopOption struct{ id, label string }

func (d *desktop) projectOptions() []desktopOption {
	out := make([]desktopOption, 0, len(d.projects.items))
	for _, p := range d.projects.items {
		out = append(out, desktopOption{p.ID, p.Slug})
	}
	return out
}

func (d *desktop) workspaceBreadcrumb() string {
	parts := []string{"Local Workspace"}
	switch d.route.Page {
	case pageSecrets:
		status := d.secrets.slot.Status
		if status.Project.Slug != "" {
			parts = append(parts, status.Project.Slug)
		}
		if status.Profile != "" && status.TaskID != "" {
			parts = append(parts, status.Profile, status.Task)
		}
	case pageFleet:
		for _, p := range d.projects.items {
			if p.ID == d.fleet.project {
				parts = append(parts, p.Slug)
				break
			}
		}
		if identity := d.fleet.data.Selected.Identity; identity != "" {
			parts = append(parts, identity)
		}
	}
	return strings.Join(parts, " / ")
}

func optionPicker(c *ui.Context, title, selected string, options []desktopOption, disabled bool, onChange func(string)) {
	labels := make([]string, len(options))
	counts := make(map[string]int, len(options))
	for _, o := range options {
		counts[o.label]++
	}
	current := "Select"
	for i, o := range options {
		labels[i] = o.label
		if counts[o.label] > 1 {
			labels[i] += " · " + o.id
		}
		if o.id == selected {
			current = labels[i]
		}
	}
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, title).TextColor(tk.Muted).FontSize(fsMeta)
		picker := ui.Select(c, &current, labels).Label(title).FillWidth().Disabled(disabled || len(labels) == 0)
		if picker.Changed() {
			if i := slices.Index(labels, current); i >= 0 {
				onChange(options[i].id)
			}
		}
	})
}

func managementColumns(c *ui.Context, main, inspector func()) {
	width, _ := c.Size()
	if width < 1000 {
		main()
		inspector()
		return
	}
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(sp4).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(sp3).Children(main)
		ui.Column(c).Width(340).Gap(sp3).Children(inspector)
	})
}

func inspectorPanel(c *ui.Context, content func()) {
	card(c).FillWidth().Children(content)
}

func labelValue(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, label).FontSize(fsMeta).TextColor(tk.Muted)
		ui.Text(c, value).FillWidth().TextColor(tk.Text).LineHeight(1.4)
	})
}

func inspectorField(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Gap(sp2).Children(func() {
		ui.Text(c, label).Width(112).FontSize(fsMeta).TextColor(tk.Muted)
		ui.Text(c, value).Grow(1).MinWidth(0).TextColor(tk.Text)
	})
}

func timeLabel(timestamp string) string {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "Not observed"
	}
	if t.Local().Format(time.DateOnly) == time.Now().Format(time.DateOnly) {
		return t.Local().Format("today 15:04:05")
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func managementError(code string) string {
	switch code {
	case "binding-missing":
		return "The requirement has no valid shared credential mapping."
	case "binding-conflict":
		return "Shared Configuration has a conflict. Resolve it in Sync first."
	case "incompatible":
		return "The requirement, Resource or shared reference is incompatible."
	case "authentication-required":
		return "Sign in or unlock the provider in this execution environment, then adopt its session."
	case "credential-storage-unavailable":
		return "Secure credential storage is unavailable in this execution environment."
	case "secret-unavailable":
		return "The referenced entry could not be confirmed readable."
	case "provider-link-unavailable":
		return "The provider management URL could not be verified. Find the entry by its displayed locator."
	case "invalid-secret-binding", "usage":
		return "Review the alias, authentication method and entry locator."
	case "selection-changed":
		return "The selected configuration changed. Refresh and confirm again."
	case "profile-not-found", "profile-required":
		return "The Profile is unavailable. Refresh and choose it again."
	case "task-not-found":
		return "The Task is unavailable. Refresh and select it again."
	case "store-busy":
		return "The local database is busy. Retry after the current writer finishes."
	default:
		return "The operation did not complete. Refresh local records and retry."
	}
}

func showManagementError(c *ui.Context, code string) {
	if code != "" {
		banner(c, toneAttention, "Action needs attention", managementError(code)+" ["+code+"]", nil)
	}
}

func failureCode(err error) string {
	if err == nil {
		return ""
	}
	return app.Classify(err).Code
}

func environmentLabel(env app.DesktopEnvironment) string {
	switch {
	case env.Local && env.Kind != "":
		return env.Device.Name + " · " + env.Kind + " (local)"
	case env.Local:
		return env.Device.Name + " (local)"
	case env.Device.Name != "":
		return env.Device.Name
	default:
		id := env.Device.ID
		if len(id) > 13 {
			id = id[:13] + "…"
		}
		kind := env.Kind
		if kind == "" {
			switch env.Device.OS {
			case "windows":
				kind = "Windows"
			case "linux":
				kind = "Linux"
			case "darwin":
				kind = "macOS"
			default:
				kind = "Device"
			}
		}
		return kind + " · " + id
	}
}

func environmentSystem(os, arch string) string {
	if os == "" {
		if arch == "" {
			return "Unknown"
		}
		return "Unknown OS · " + arch
	}
	switch os {
	case "darwin":
		os = "macOS"
	case "windows":
		os = "Windows"
	case "linux":
		os = "Linux"
	}
	return strings.Join([]string{os, arch}, " · ")
}

func nowLabel() string { return time.Now().UTC().Format(time.RFC3339Nano) }
