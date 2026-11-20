package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func init() { desktopPages[pageFleet] = (*desktop).fleetPage }

type fleetView struct {
	project, resource string
	data              app.DesktopFleet
	ready, wait       bool
	issue, probeIssue string
	selected          int
	devices           ui.ListState
	scroll            ui.ScrollState
	confirm           bool
	receipt           *app.Observation
}

func (d *desktop) fleetTarget() (string, string) {
	project := d.validProject(valueOr(d.route.ProjectID, d.fleet.project))
	resource := d.route.RecordID
	if resource == "" && project == d.fleet.project {
		resource = d.fleet.resource
	}
	return project, resource
}

func (d *desktop) loadFleet(project, resource string) {
	f := &d.fleet
	if project == "" || f.wait || d.window == nil {
		return
	}
	if project != f.project {
		*f = fleetView{project: project}
	}
	if f.resource != resource {
		f.receipt, f.probeIssue = nil, ""
	}
	f.wait, f.ready, f.issue = true, true, ""
	desktopWork(d, func(ctx context.Context) (app.DesktopFleet, error) {
		return d.core.DesktopFleetView(ctx, project, resource)
	}, func(data app.DesktopFleet, err error) {
		if err == nil && data.Selected.ID == "" && resource != "" {
			if d.route.Page == pageFleet && d.route.RecordID == resource && d.validProject(valueOr(d.route.ProjectID, f.project)) == project {
				d.route.RecordID = ""
			}
			if len(data.Resources) > 0 {
				f.wait = false
				d.loadFleet(project, "")
				return
			}
		}
		f.data, f.resource, f.wait, f.issue = data, data.Selected.ID, false, failureCode(err)
	})
}

func (d *desktop) fleetPage(c *ui.Context) {
	pageHeader(c, "Resources & environments", "Inspect routes and observations without confusing unknown evidence with a successful check.", nil)
	if !d.managementReady(c) {
		return
	}
	f := &d.fleet
	project, resource := d.fleetTarget()
	if !f.ready || project != f.project || (resource != "" && resource != f.resource && f.issue == "") {
		d.loadFleet(project, resource)
	}
	resources := make([]desktopOption, 0, len(f.data.Resources))
	for _, r := range f.data.Resources {
		resources = append(resources, desktopOption{r.ID, r.Identity})
	}
	card(c).Children(func() {
		ui.Row(c).FillWidth().Gap(sp3).Wrap().Children(func() {
			optionPicker(c, "Project", f.project, d.projectOptions(), f.wait, func(id string) {
				d.navigate(desktopRoute{Page: pageFleet, ProjectID: id})
				d.loadFleet(id, "")
			})
			optionPicker(c, "Resource", f.resource, resources, f.wait, func(id string) {
				d.navigate(desktopRoute{Page: pageFleet, ProjectID: f.project, RecordID: id})
				d.loadFleet(f.project, id)
			})
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "Stage").TextColor(tk.Muted)
				ui.Text(c, valueOr(f.data.Selected.Stage, "—")).Bold()
			})
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "Execution environments").TextColor(tk.Muted)
				if f.issue == "" && len(f.data.Devices) > 0 {
					ui.Text(c, fmt.Sprintf("%d", len(f.data.Devices))).Bold()
				} else {
					ui.Text(c, "—")
				}
			})
		})
	})
	if f.data.Selected.ID != "" {
		scopeBar(c, []scopeItem{{label: "Resource", value: f.data.Selected.Identity, id: f.data.Selected.ID, icon: icServer}}, nil)
	}
	showManagementError(c, f.issue)
	if f.wait {
		muted(c, "Reading the selected Resource…")
	}
	if f.receipt != nil {
		o := f.receipt
		banner(c, toneOk, "Local observation recorded", observationLabel(o)+" · "+o.Scope.Target+" · "+timeLabel(o.ObservedAt), nil)
	}
	if f.issue != "" || len(f.data.Devices) == 0 {
		return
	}
	if len(f.data.Resources) == 0 {
		muted(c, "This Project has no Resource configuration.")
	}
	managementColumns(c, func() { d.fleetTable(c) }, func() { d.fleetInspector(c) })
}

func (d *desktop) fleetTable(c *ui.Context) {
	f := &d.fleet
	card(c).Padding(0).Clip().Children(func() {
		ui.Row(c).Padding(sp4, sp4, 0, sp4).Children(func() {
			cardHeader(c, icLaptop, "Execution environments", "Observations belong to the environment that produced them.", nil)
		})
		const rowHeight = 36
		f.devices.RowHeight = rowHeight
		f.devices.Selected = &f.selected
		f.devices.Key = func(i int) any { return f.data.Devices[i].Device.ID }
		f.devices.Label = func(i int) string { return environmentLabel(f.data.Devices[i]) }
		ui.Table(c, &f.devices, []ui.TableColumn{
			{Title: "Environment"}, {Title: "System", Width: 130}, {Title: "Observation", Width: 130}, {Title: "Observed", Width: 160},
		}, len(f.data.Devices), func(i, col int) {
			env := f.data.Devices[i]
			switch col {
			case 0:
				ui.Text(c, environmentLabel(env)).NoWrap().Ellipsis("…")
			case 1:
				ui.Text(c, environmentSystem(env.Device.OS, env.Device.Arch)).NoWrap().Ellipsis("…")
			case 2:
				statusLabel(c, f.latestLabel(env))
			case 3:
				stamp := ""
				if observation := latestObservation(env); observation != nil {
					stamp = observation.ObservedAt
				}
				ui.Text(c, timeLabel(stamp)).NoWrap().Ellipsis("…")
			}
		}).FillWidth().Height(float32(min(len(f.data.Devices), 6))*rowHeight + 40)
	})
	if f.selected < 0 || f.selected >= len(f.data.Devices) {
		f.selected = 0
	}
	env := f.data.Devices[f.selected]
	resource := f.data.Selected
	if resource.ID == "" {
		return
	}
	card(c).Children(func() {
		cardHeader(c, icServer, "Route and observations", resource.Identity+" · "+resource.Purpose, nil)
		kvGrid(c, []kvRow{
			{k: "Configured endpoint", v: valueOr(strings.Join(resource.Endpoints, " · "), "No shared endpoint"), mono: true},
			{k: "Applicability", v: valueOr(strings.Join(resource.Platforms, " · "), "All platforms")},
			{k: "Required network", v: valueOr(resource.Network, "Any network")},
		})
		if env.Local {
			detail(c, "Local route", routeLabel(f.data.Route))
		} else {
			detail(c, "Route on this device", "Unknown · inspect it in that execution environment")
		}
		ui.Divider(c)
		section(c, "Recent observations")
		if len(env.Observations) == 0 {
			muted(c, "No observations for this Resource in the selected environment.")
		}
		for _, o := range env.Observations {
			qualifier := ""
			if o.Scope.ResourceRevision != resource.Revision {
				qualifier = " · previous Resource revision"
			} else if env.Local && f.data.Probe != nil && o.Scope != f.data.Probe.Scope {
				qualifier = " · previous route"
			}
			labelValue(c, timeLabel(o.ObservedAt)+" · "+o.Tool+qualifier, observationLabel(&o)+" · "+o.Scope.Target+observationNetwork(o))
		}
	})
}

func (d *desktop) fleetInspector(c *ui.Context) {
	f := &d.fleet
	env := f.data.Devices[f.selected]
	card(c).Children(func() {
		cardHeader(c, icLaptop, environmentLabel(env), "Selected execution environment", nil)
		statusLabel(c, f.latestLabel(env))
		labelValue(c, "Device ID", env.Device.ID)
		inspectorField(c, "System", environmentSystem(env.Device.OS, env.Device.Arch))
		if !env.Local {
			labelValue(c, "Provider authentication", "Session state is unknown on this device")
			muted(c, "Sign in or probe inside that execution environment; no local result is substituted.")
			return
		}
		labelValue(c, "Provider authentication", "Checked separately through Credential bindings")
		if f.data.Selected.ID == "" {
			return
		}
		ui.Divider(c)
		if f.data.Probe == nil {
			labelValue(c, "Probe unavailable", routeLabel(f.data.Route))
			showManagementError(c, f.probeIssue)
			return
		}
		probe := *f.data.Probe
		labelValue(c, probeTargetLabel(probe.Scope), probe.Scope.Target)
		showManagementError(c, f.probeIssue)
		ui.PrimaryButton(c, "Probe TCP on this device…").Disabled(f.wait).FillWidth().OnClick(func() { f.confirm = true })
		if f.confirm {
			target := fmt.Sprintf("%s will open one TCP connection to %s. The result is recorded for this Resource and environment. No credentials are sent.", env.Device.Name, probe.Scope.Target)
			if probe.Scope.Kind == "proxy-endpoint" {
				target += "\n\nThis checks only the proxy TCP port, not the service behind it."
			}
			if ui.AlertDialog(c, &f.confirm, "Confirm local TCP probe", target, "Cancel", "Start probe") == 1 {
				d.probeResource(probe)
			}
		}
	})
}

func (d *desktop) probeResource(probe app.DesktopResourceProbe) {
	f := &d.fleet
	f.wait, f.probeIssue = true, ""
	desktopWork(d, func(ctx context.Context) (app.Observation, error) {
		return d.core.ProbeDesktopResource(ctx, probe)
	}, func(observation app.Observation, err error) {
		f.wait, f.probeIssue = false, failureCode(err)
		if err == nil {
			f.receipt = &observation
			f.scroll.Y = 0
		}
		d.loadFleet(f.project, f.resource)
	})
}

func latestObservation(env app.DesktopEnvironment) *app.Observation {
	if len(env.Observations) == 0 {
		return nil
	}
	return &env.Observations[0]
}

func (f *fleetView) latestLabel(env app.DesktopEnvironment) string {
	o := latestObservation(env)
	if o != nil && (o.Scope.ResourceRevision != f.data.Selected.Revision || env.Local && (f.data.Probe == nil || f.data.Probe.Scope != o.Scope)) {
		return "Previous observation"
	}
	return observationLabel(o)
}

func observationLabel(o *app.Observation) string {
	if o == nil || !slices.Contains([]string{"reachable", "unreachable"}, o.Status) || o.Tool != "tcp-connect" {
		return "Unknown"
	}
	label := "TCP reachable"
	if o.Status == "unreachable" {
		label = "TCP unreachable"
	}
	if o.Scope.Kind == "proxy-endpoint" {
		label = "Proxy " + label
	}
	return label
}

func observationNetwork(o app.Observation) string {
	if o.Scope.Network == "" {
		return ""
	}
	return " · " + o.Scope.Network
}

func probeTargetLabel(scope app.ObservationScope) string {
	if scope.Kind == "proxy-endpoint" {
		return "Probe target · proxy port"
	}
	return "Probe target · service port"
}

func routeLabel(r app.ResourceRoute) string {
	switch r.Status {
	case "resolved":
		source := "Shared Resource"
		if r.Source == "device-local" {
			source = "Device-local override"
		}
		path := r.Endpoint
		if r.Proxy != "" {
			path = r.Proxy + " → " + path
		}
		return path + " · " + source
	case "platform-unsupported":
		return "Resource does not apply to this platform"
	case "network-mismatch":
		return "The current environment does not match the required network"
	case "route-ambiguous":
		return "Multiple candidate endpoints; configure a local route first"
	case "route-missing":
		return "No locally usable endpoint"
	default:
		return "Unknown"
	}
}
