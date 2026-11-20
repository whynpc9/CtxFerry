package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/bitwarden"
	"github.com/whynpc9/CtxFerry/internal/provider/infisical"
)

func init() { desktopPages[pageSecrets] = (*desktop).secretsPage }

type secretsView struct {
	project, profile, task, requirement string
	catalog                             app.DesktopCredentialCatalog
	slot                                app.DesktopCredentialSlot
	ready, wait                         bool
	issue, receipt                      string
	slotIssue, providerIssue            string
	authSeen, entrySeen                 string
	receiptTarget                       app.SecretTarget
	scroll                              ui.ScrollState

	draft   *bindingDraft
	confirm bool
}

func (s *secretsView) target() app.SecretTarget {
	return app.SecretTarget{Profile: s.profile, Task: s.task, Requirement: s.requirement}
}

func (s *secretsView) setReceipt(message string) {
	s.receipt, s.receiptTarget = message, s.target()
}

func (d *desktop) openCredential(project, profile, task, requirement string) {
	d.secrets = secretsView{project: project, profile: profile, task: task, requirement: requirement}
	d.navigate(desktopRoute{Page: pageSecrets, ProjectID: project})
	d.loadCredentials(project)
}

func (d *desktop) loadCredentials(project string) {
	s := &d.secrets
	if project == "" || s.wait || d.window == nil {
		return
	}
	if s.project != project {
		*s = secretsView{project: project}
	}
	s.ready, s.wait, s.issue = true, true, ""
	desktopWork(d, func(ctx context.Context) (app.DesktopCredentialCatalog, error) {
		return d.core.DesktopCredentials(ctx, project)
	}, func(catalog app.DesktopCredentialCatalog, err error) {
		s.wait, s.issue, s.catalog = false, failureCode(err), catalog
		if err != nil {
			s.slot = app.DesktopCredentialSlot{}
			return
		}
		if !slices.Contains(catalog.Profiles, s.profile) {
			s.profile = ""
			if len(catalog.Profiles) > 0 {
				s.profile = catalog.Profiles[0]
			}
		}
		if len(catalog.Tasks) == 0 {
			s.task, s.requirement, s.slot = "", "", app.DesktopCredentialSlot{}
			return
		}
		d.chooseCredentialTask(s.task)
	})
}

func (d *desktop) chooseCredentialTask(id string) {
	s := &d.secrets
	i := slices.IndexFunc(s.catalog.Tasks, func(t app.DesktopCredentialTask) bool { return t.ID == id })
	if i < 0 {
		i = 0
	}
	task := s.catalog.Tasks[i]
	if task.ID != s.task || !slices.ContainsFunc(task.Slots, func(req app.DesktopCredentialRequirement) bool {
		return req.Name == s.requirement
	}) {
		s.requirement = task.Slots[0].Name
	}
	s.task = task.ID
	d.loadSlot()
}

func (d *desktop) loadSlot() {
	s := &d.secrets
	s.slot, s.slotIssue, s.providerIssue = app.DesktopCredentialSlot{}, "", ""
	s.authSeen, s.entrySeen, s.draft, s.confirm = "", "", nil, false
	if s.profile == "" || s.task == "" || s.requirement == "" || d.window == nil {
		return
	}
	s.wait = true
	project, target := s.project, s.target()
	desktopWork(d, func(ctx context.Context) (app.DesktopCredentialSlot, error) {
		return d.core.DesktopSecretSlot(ctx, project, target)
	}, func(slot app.DesktopCredentialSlot, err error) {
		s.slot, s.slotIssue, s.wait = slot, failureCode(err), false
	})
}

func (d *desktop) secretsPage(c *ui.Context) {
	pageHeader(c, "Credential bindings", "Map shared references without exposing values; check each local requirement independently.", nil)
	if !d.managementReady(c) {
		return
	}
	s := &d.secrets
	project := d.validProject(valueOr(d.route.ProjectID, s.project))
	if !s.ready || project != s.project {
		d.loadCredentials(project)
	}
	var profiles, tasks, slots []desktopOption
	for _, name := range s.catalog.Profiles {
		profiles = append(profiles, desktopOption{name, name})
	}
	var requirement app.DesktopCredentialRequirement
	for _, task := range s.catalog.Tasks {
		tasks = append(tasks, desktopOption{task.ID, task.Name})
		if task.ID == s.task {
			for _, req := range task.Slots {
				slots = append(slots, desktopOption{req.Name, req.Name})
				if req.Name == s.requirement {
					requirement = req
				}
			}
		}
	}
	card(c).Children(func() {
		ui.Row(c).FillWidth().Gap(sp3).Wrap().Children(func() {
			optionPicker(c, "Project", s.project, d.projectOptions(), s.wait, func(id string) {
				d.navigate(desktopRoute{Page: pageSecrets, ProjectID: id})
				d.loadCredentials(id)
			})
			optionPicker(c, "Profile", s.profile, profiles, s.wait, func(id string) { s.profile = id; d.loadSlot() })
			optionPicker(c, "Task", s.task, tasks, s.wait, d.chooseCredentialTask)
			optionPicker(c, "Requirement", s.requirement, slots, s.wait, func(id string) { s.requirement = id; d.loadSlot() })
		})
	})
	if s.slot.Status.TaskID != "" {
		scopeBar(c, []scopeItem{
			{label: "Project", value: s.slot.Status.Project.Slug, id: s.project, icon: icFolder},
			{label: "Profile", value: s.profile, icon: icLayers},
			{label: "Task", value: s.slot.Status.Task, id: s.slot.Status.TaskID, icon: icTasks},
			{label: "Requirement", value: s.requirement, icon: icKey},
		}, nil)
	}
	showManagementError(c, s.issue)
	if s.wait {
		muted(c, "Reading the selected credential requirement…")
	}
	if s.receipt != "" && s.receiptTarget == s.target() {
		banner(c, toneOk, "Local check complete", s.receipt, nil)
	}
	switch {
	case s.issue != "":
		return
	case !s.wait && len(profiles) == 0:
		muted(c, "This Project has no shared Profiles.")
	case !s.wait && len(tasks) == 0:
		muted(c, "This Project has no Tasks declaring credential requirements.")
	case s.slot.Status.TaskID != "":
		managementColumns(c, func() { d.secretConditions(c, requirement) }, func() { d.secretInspector(c) })
	default:
		showManagementError(c, s.slotIssue)
		showManagementError(c, s.slot.Issue)
		if !s.wait && s.profile != "" && s.task != "" {
			d.unmappedSlotHandoff(c, requirement)
		}
	}
}

type conditionRow struct{ name, status, stamp string }

func (d *desktop) secretConditions(c *ui.Context, requirement app.DesktopCredentialRequirement) {
	s := &d.secrets
	status := s.slot.Status
	card(c).Children(func() {
		cardHeader(c, icKey, s.requirement, requirement.Purpose+" · "+requirement.Type, nil)
		bound := "Not mapped"
		if status.Bound {
			bound = "Mapped"
		} else if s.slot.Issue != "" && s.slot.Issue != "binding-missing" {
			bound = "Needs attention"
		}
		rows := []conditionRow{
			{"Shared credential binding", bound, "Current configuration"},
			{"Provider authentication", authenticationLabel(status), timeLabel(s.authSeen)},
			{"Entry readable", boolLabel(status.EntryRetrievable, "Readable", "Not readable"), timeLabel(s.entrySeen)},
			{"Resource reachability", boolLabel(status.ResourceReachable, "TCP reachable", "TCP unreachable"), timeLabel(status.ReachabilitySeenAt)},
			{"Task run", "No run evidence", "—"},
		}
		ui.Table(c, nil, []ui.TableColumn{{Title: "Condition"}, {Title: "State", Width: 140}, {Title: "Observed", Width: 165}}, len(rows), func(i, col int) {
			r := rows[i]
			if col == 1 {
				statusLabel(c, r.status)
				return
			}
			ui.Text(c, []string{r.name, r.status, r.stamp}[col])
		}).FillWidth().Height(204)
	})
	showManagementError(c, s.slot.Issue)
	showManagementError(c, s.slotIssue)
	binding := "Not mapped"
	if status.Binding != nil {
		binding = status.Binding.Alias
	}
	card(c).Children(func() {
		cardHeader(c, icLink, "Shared reference · "+binding, "A mapping changes the shared reference, never the provider-held value.", nil)
		label := "Map shared binding…"
		if status.Bound {
			label = "Change shared binding…"
		}
		ui.Button(c, label).Disabled(s.wait || s.slot.Resource.ID == "" || s.slot.Issue == "binding-conflict").OnClick(func() {
			s.draft = newBindingDraft(s.slot)
		})
	})
	if s.draft != nil {
		d.bindingEditor(c)
	}
	if resource := s.slot.Resource; resource.ID != "" {
		card(c).Children(func() {
			cardHeader(c, icServer, "Resource match", "This requirement resolves through its declared Resource slot.", nil)
			labelValue(c, "Mapped Resource", resource.Identity+" · "+resource.Stage)
			labelValue(c, "Purpose", resource.Purpose)
			linkButton(c, "Open Resource "+resource.Identity, icArrowR).AlignSelf(ui.Start).Disabled(d.fleet.wait).OnClick(func() {
				d.navigate(desktopRoute{Page: pageFleet, ProjectID: s.project, RecordID: resource.ID})
				d.loadFleet(s.project, resource.ID)
			})
		})
	}
}

func (d *desktop) unmappedSlotHandoff(c *ui.Context, requirement app.DesktopCredentialRequirement) {
	s := &d.secrets
	card(c).Children(func() {
		cardHeader(c, icLink, "Resolve the Resource slot first", "A credential mapping must match the Resource mapped to this requirement's slot.", nil)
		detail(c, "Required slot", s.slot.Status.ResourceSlot)
		muted(c, "Choose the intended Resource in Resources & environments, copy its full Resource UUID into this command, then run it and Refresh.")
		d.commandBlock(c, desktopCommand(d.core.DataDir, "profile", "map-resource", s.profile, s.task, s.slot.Status.ResourceSlot, "<resource-uuid>"), true)
		d.afterRun(c, "After the slot is mapped and reread, the compatible shared reference preview becomes available.")
		ui.Button(c, "Open Resources & environments").AlignSelf(ui.Start).OnClick(func() {
			d.navigate(desktopRoute{Page: pageFleet, ProjectID: s.project})
		})
	})
}

func (d *desktop) secretInspector(c *ui.Context) {
	s := &d.secrets
	binding := s.slot.Status.Binding
	card(c).Children(func() {
		device := d.data.Device.Name + " · local"
		system := environmentSystem(d.data.Device.OS, d.data.Device.Arch)
		if binding == nil {
			cardHeader(c, icLaptop, device, system, nil)
			muted(c, "Map a shared reference to check authentication in this execution environment.")
			return
		}
		profile := binding.ProviderProfile
		cardHeader(c, icKey, providerLabel(profile.Provider)+" authentication", device+" · "+system, func() {
			statusLabel(c, authenticationLabel(s.slot.Status))
		})
		inspectorField(c, "Provider profile", profile.Name+" · "+profile.Method)
		inspectorField(c, "Binding alias", binding.Alias)
		if binding.Locator.EntryID != "" {
			inspectorField(c, "Entry ID", binding.Locator.EntryID)
		} else {
			inspectorField(c, "Backend Project · environment", binding.Locator.ProjectID+" · "+binding.Locator.Environment)
			inspectorField(c, "Path · key", binding.Locator.Path+" · "+binding.Locator.Key)
		}
		ui.Divider(c)
		d.providerActions(c, profile)
	})
}

func providerLabel(name string) string {
	switch name {
	case infisical.Name:
		return "Infisical"
	case bitwarden.Name:
		return "Bitwarden"
	default:
		return name
	}
}

func (d *desktop) providerActions(c *ui.Context, profile provider.Profile) {
	s := &d.secrets
	authed := s.slot.Status.Authenticated
	if authed == nil || !*authed {
		var help string
		switch {
		case profile.Provider == infisical.Name && profile.Method == "personal":
			help = "Sign in with the Infisical CLI in this environment, then adopt that session in CtxFerry."
		case profile.Provider == bitwarden.Name:
			help = "Sign in and unlock the Bitwarden CLI, then use ctxferry provider login to adopt the session."
		case profile.Method == "universal-auth":
			help = "Authentication uses the machine identity configured in this execution environment."
		}
		if help != "" {
			muted(c, help)
		}
	}
	if s.providerIssue != "authentication-required" || authed == nil {
		showManagementError(c, s.providerIssue)
	}
	if authed != nil && *authed {
		ui.PrimaryButton(c, "Check entry readability").Disabled(s.wait).FillWidth().OnClick(func() { d.checkBinding(true) })
	}
	ui.Row(c).FillWidth().Gap(sp2).Children(func() {
		ui.Button(c, "Check local authentication").Disabled(s.wait).Grow(1).OnClick(func() { d.checkBinding(false) })
		ui.Button(c, "Adopt configured session").Disabled(s.wait).Grow(1).OnClick(d.loginForBinding)
	})
	label := "Manage entry in provider ↗"
	if profile.Provider == infisical.Name {
		label = "Open Infisical console ↗"
	}
	ui.Button(c, label).Disabled(s.wait).FillWidth().OnClick(func() { d.openProvider(c.Services()) })
}

func locatorFields(c *ui.Context, locator provider.Locator) {
	if locator.EntryID != "" {
		labelValue(c, "Entry ID", locator.EntryID)
		return
	}
	labelValue(c, "Backend Project · environment", locator.ProjectID+" · "+locator.Environment)
	labelValue(c, "Path · key", locator.Path+" · "+locator.Key)
}

func boolLabel(b *bool, yes, no string) string {
	if b == nil {
		return "Unknown"
	}
	if *b {
		return yes
	}
	return no
}

func authenticationLabel(s app.SecretStatus) string {
	if s.Authentication != nil {
		switch s.Authentication.State {
		case provider.Authenticated:
			return "Authenticated"
		case provider.Locked:
			return "Unlock required"
		case provider.Expired:
			return "Session expired"
		case provider.SignedOut:
			return "Sign-in required"
		}
	}
	return boolLabel(s.Authenticated, "Authenticated", "Sign-in required")
}

func statusLabel(c *ui.Context, label string) {
	t := toneUnknown
	switch label {
	case "Mapped", "Authenticated", "Readable", "TCP reachable", "Proxy TCP reachable":
		t = toneOk
	case "Sign-in required", "Unlock required", "Session expired", "Not mapped", "Needs attention", "Not readable", "TCP unreachable", "Proxy TCP unreachable", "Previous observation":
		t = toneAttention
	}
	pill(c, t, label)
}

func (s *secretsView) invalidateProviderResult() {
	s.slot.Status.Authentication, s.slot.Status.Authenticated, s.slot.Status.EntryRetrievable = nil, nil, nil
	s.authSeen, s.entrySeen = "", ""
}

func (d *desktop) checkBinding(entry bool) {
	s := &d.secrets
	expected := s.slot
	s.wait, s.providerIssue = true, ""
	s.setReceipt("")
	desktopWork(d, func(ctx context.Context) (app.SecretStatus, error) {
		return d.core.CheckDesktopSecret(ctx, expected, entry)
	}, func(status app.SecretStatus, err error) {
		s.wait, s.providerIssue = false, failureCode(err)
		s.invalidateProviderResult()
		if s.providerIssue == "selection-changed" {
			d.loadSlot()
			s.providerIssue = "selection-changed"
			return
		}
		s.slot.Status.Authentication, s.slot.Status.Authenticated = status.Authentication, status.Authenticated
		s.slot.Status.EntryRetrievable = status.EntryRetrievable
		s.slot.Status.ResourceReachable, s.slot.Status.ReachabilitySeenAt = status.ResourceReachable, status.ReachabilitySeenAt
		if status.Authenticated != nil {
			s.authSeen = nowLabel()
		}
		if status.EntryRetrievable != nil {
			s.entrySeen = nowLabel()
			result := boolLabel(status.EntryRetrievable, "Readable", "Not readable")
			s.setReceipt(fmt.Sprintf("%s · entry %s · %s / %s / %s / %s", d.data.Device.Name, result,
				status.Project.Slug, status.Profile, status.Task, status.Requirement))
		}
	})
}

func (d *desktop) loginForBinding() {
	s := &d.secrets
	expected := s.slot
	s.wait, s.providerIssue = true, ""
	s.setReceipt("")
	desktopWork(d, func(ctx context.Context) (app.ProviderLoginResult, error) {
		return d.core.LoginDesktopProvider(ctx, expected)
	}, func(result app.ProviderLoginResult, err error) {
		s.wait, s.providerIssue = false, failureCode(err)
		s.invalidateProviderResult()
		if result.Authentication.State != "" && result.Authentication.State != provider.Authenticated {
			s.slot.Status.Authentication = &result.Authentication
			s.slot.Status.Authenticated = new(false)
			s.authSeen = nowLabel()
		}
		if err == nil {
			s.slot.Status.Authentication = &result.Authentication
			s.slot.Status.Authenticated = new(true)
			s.authSeen = nowLabel()
			s.setReceipt(fmt.Sprintf("Adopted the %s / %s session on %s.", providerLabel(result.Profile.Provider), result.Profile.Name, d.data.Device.Name))
		}
	})
}

func (d *desktop) openProvider(services ui.Services) {
	s := &d.secrets
	expected := s.slot
	s.wait, s.providerIssue = true, ""
	desktopWork(d, func(ctx context.Context) (string, error) {
		return d.core.DesktopSecretManagementURL(ctx, expected)
	}, func(url string, err error) {
		s.wait, s.providerIssue = false, failureCode(err)
		if err == nil {
			if ui.SafeToOpen(url) {
				services.OpenURL(url)
			} else {
				s.providerIssue = "provider-link-unavailable"
			}
		}
	})
}
