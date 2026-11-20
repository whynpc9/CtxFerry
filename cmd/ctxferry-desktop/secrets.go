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
	pageHeading(c, "凭据绑定", "逐项检查任务运行需要的条件")
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
	metrics(c, func() {
		optionPicker(c, "项目", s.project, d.projectOptions(), s.wait, func(id string) {
			d.navigate(desktopRoute{Page: pageSecrets, ProjectID: id})
			d.loadCredentials(id)
		})
		optionPicker(c, "配置方案", s.profile, profiles, s.wait, func(id string) {
			s.profile = id
			d.loadSlot()
		})
		optionPicker(c, "任务", s.task, tasks, s.wait, d.chooseCredentialTask)
		optionPicker(c, "槽位", s.requirement, slots, s.wait, func(id string) {
			s.requirement = id
			d.loadSlot()
		})
	})
	showManagementError(c, s.issue)
	if s.wait {
		ui.Text(c, "正在处理当前凭据请求…").TextColor(muted)
	}
	if s.receipt != "" && s.receiptTarget == s.target() {
		ui.Text(c, s.receipt).FillWidth().Padding(10, 12).Background(panel).Radius(6).TextColor(ink)
	}
	switch {
	case s.issue != "":
		return
	case !s.wait && len(profiles) == 0:
		ui.Text(c, "此项目暂无共享配置方案。")
	case !s.wait && len(tasks) == 0:
		ui.Text(c, "此项目暂无声明凭据要求的任务。")
	case s.slot.Status.TaskID != "":
		managementColumns(c, func() {
			d.secretConditions(c, requirement)
		}, func() {
			d.secretInspector(c)
		})
	default:
		showManagementError(c, s.slotIssue)
		showManagementError(c, s.slot.Issue)
	}
}

type conditionRow struct{ name, status, stamp string }

func (d *desktop) secretConditions(c *ui.Context, requirement app.DesktopCredentialRequirement) {
	s := &d.secrets
	status := s.slot.Status
	section(c, s.requirement)
	bound := "未绑定"
	if status.Bound {
		bound = "已绑定"
	} else if s.slot.Issue != "" && s.slot.Issue != "binding-missing" {
		bound = "需处理"
	}
	rows := []conditionRow{
		{"共享槽位绑定", bound, "当前配置"},
		{"Provider 认证", authenticationLabel(status), timeLabel(s.authSeen)},
		{"条目可读取", boolLabel(status.EntryRetrievable, "可读取", "不可读取"), timeLabel(s.entrySeen)},
		{"资源可达", boolLabel(status.ResourceReachable, "TCP 可达", "TCP 不可达"), timeLabel(status.ReachabilitySeenAt)},
		{"任务测试", "无运行证据", "—"},
	}
	ui.Table(c, nil, []ui.TableColumn{{Title: "条件"}, {Title: "状态", Width: 126}, {Title: "观察时间", Width: 158}}, len(rows), func(i, col int) {
		r := rows[i]
		if col == 1 {
			statusLabel(c, r.status)
			return
		}
		ui.Text(c, []string{r.name, r.status, r.stamp}[col])
	}).FillWidth().Height(204)
	showManagementError(c, s.slot.Issue)
	showManagementError(c, s.slotIssue)
	binding := "未绑定"
	if status.Binding != nil {
		binding = status.Binding.Alias
	}
	ui.Row(c).FillWidth().Gap(12).Padding(12).Background(panel).Radius(6).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "当前共享引用 · "+binding).Bold()
			ui.Text(c, requirement.Purpose+" · "+requirement.Type).TextColor(muted)
		})
		label := "映射共享绑定…"
		if status.Bound {
			label = "更换共享绑定…"
		}
		if ui.Button(c, label).Disabled(s.wait || s.slot.Resource.ID == "" || s.slot.Issue == "binding-conflict").Clicked() {
			s.draft = newBindingDraft(s.slot)
		}
	})
	if s.draft != nil {
		d.bindingEditor(c)
	}
	if resource := s.slot.Resource; resource.ID != "" {
		ui.Divider(c)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			section(c, "关联资源")
			ui.Spacer(c)
			if ui.Button(c, "打开资源观察").Disabled(d.fleet.wait).Clicked() {
				d.navigate(desktopRoute{Page: pageFleet, ProjectID: s.project, RecordID: resource.ID})
				d.loadFleet(s.project, resource.ID)
			}
		})
		labelValue(c, "目标资源", resource.Identity+" · "+resource.Stage)
		labelValue(c, "资源用途", resource.Purpose)
	}
}

func (d *desktop) secretInspector(c *ui.Context) {
	s := &d.secrets
	binding := s.slot.Status.Binding
	inspectorPanel(c, func() {
		device := d.data.Device.Name + "（本机）"
		system := environmentSystem(d.data.Device.OS, d.data.Device.Arch)
		if binding == nil {
			section(c, device)
			inspectorField(c, "系统", system)
			ui.Divider(c)
			ui.Text(c, "映射共享引用后可检查当前环境的认证状态。").FillWidth()
			return
		}
		profile := binding.ProviderProfile
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			section(c, providerLabel(profile.Provider)+" 认证")
			ui.Spacer(c)
			statusLabel(c, authenticationLabel(s.slot.Status))
		})
		ui.Text(c, device+" · "+system).FillWidth().TextColor(muted)
		inspectorField(c, "Provider 配置", profile.Name+" · "+profile.Method)
		inspectorField(c, "绑定别名", binding.Alias)
		if binding.Locator.EntryID != "" {
			inspectorField(c, "条目 ID", binding.Locator.EntryID)
		} else {
			inspectorField(c, "后端项目 · 环境", binding.Locator.ProjectID+" · "+binding.Locator.Environment)
			inspectorField(c, "路径 · 键", binding.Locator.Path+" · "+binding.Locator.Key)
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
			help = "在本机通过 Infisical CLI 登录后，更新 CtxFerry 会话。"
		case profile.Provider == bitwarden.Name:
			help = "在本机通过 Bitwarden CLI 登录并解锁，再用 ctxferry provider login 载入会话。"
		case profile.Method == "universal-auth":
			help = "认证使用当前环境预先配置的机器身份。"
		}
		if help != "" {
			ui.Text(c, help).FillWidth().TextColor(muted)
		}
	}
	if s.providerIssue != "authentication-required" || authed == nil {
		showManagementError(c, s.providerIssue)
	}
	if authed != nil && *authed {
		if ui.PrimaryButton(c, "检查条目可读性").Disabled(s.wait).FillWidth().Clicked() {
			d.checkBinding(true)
		}
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if ui.Button(c, "检查本机认证").Disabled(s.wait).Grow(1).Clicked() {
			d.checkBinding(false)
		}
		if ui.Button(c, "更新本机会话").Disabled(s.wait).Grow(1).Clicked() {
			d.loginForBinding()
		}
	})
	label := "在 Provider 中管理条目 ↗"
	if profile.Provider == infisical.Name {
		label = "打开 Infisical 控制台 ↗"
	}
	if ui.Button(c, label).Disabled(s.wait).FillWidth().Clicked() {
		d.openProvider(c.Services())
	}
}

func locatorFields(c *ui.Context, locator provider.Locator) {
	if locator.EntryID != "" {
		labelValue(c, "条目 ID", locator.EntryID)
		return
	}
	labelValue(c, "后端项目 · 环境", locator.ProjectID+" · "+locator.Environment)
	labelValue(c, "路径 · 键", locator.Path+" · "+locator.Key)
}

func boolLabel(b *bool, yes, no string) string {
	if b == nil {
		return "未知"
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
			return "已认证"
		case provider.Locked:
			return "需解锁"
		case provider.Expired:
			return "会话已过期"
		case provider.SignedOut:
			return "需登录"
		}
	}
	return boolLabel(s.Authenticated, "已认证", "需登录")
}

func statusLabel(c *ui.Context, label string) {
	bg, fg := ui.Hex("#e9eeef"), ui.Hex("#394d55")
	switch label {
	case "已绑定", "已认证", "可读取", "TCP 可达", "代理 TCP 可达":
		bg, fg = ui.Hex("#e0eee7"), ui.Hex("#225844")
	case "需登录", "需解锁", "会话已过期", "未绑定", "需处理", "不可读取", "TCP 不可达", "代理 TCP 不可达":
		bg, fg = ui.Hex("#fcecca"), ui.Hex("#764703")
	}
	ui.Text(c, label).Bold().Padding(3, 7).Radius(5).TextColor(fg).Background(bg)
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
			result := boolLabel(status.EntryRetrievable, "可读取", "不可读取")
			s.setReceipt(fmt.Sprintf("%s 本机条目检查 · %s · %s / %s / %s / %s", d.data.Device.Name, result,
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
			s.setReceipt(fmt.Sprintf("已更新 %s 上的 %s / %s 会话。", d.data.Device.Name, providerLabel(result.Profile.Provider), result.Profile.Name))
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
