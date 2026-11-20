package main

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

type overviewState struct {
	list     ui.ListState
	selected int
}

type overviewProject struct {
	id, name           string
	paths              []string
	pending, conflicts int
}

func (d *desktop) overview(c *ui.Context) {
	data := d.data
	projects := overviewProjects(data)
	count, pending, conflicts, stamp := "未知", "未知", "未知", "未知"
	if data.BindingsErr == nil {
		count = strconv.Itoa(len(projects))
	}
	if data.SyncErr == nil {
		pending, conflicts = strconv.Itoa(data.QueuePending), strconv.Itoa(len(data.Sync.Conflicts))
		stamp = observedTime(data.Sync.LastSyncAt)
	}
	pageHeading(c, "设备总览", data.Device.Name+" · "+data.Device.OS+" · "+data.Device.Arch)
	metrics(c, func() {
		metric(c, "已绑定项目", count, nil)
		metric(c, "待发布队列", pending, d.queueLink(c, queuePending, "", "查看队列", data.SyncErr == nil && data.QueuePending > 0))
		metric(c, "未解决冲突", conflicts, d.queueLink(c, queueConflicts, "", "查看冲突", data.SyncErr == nil && len(data.Sync.Conflicts) > 0))
		metric(c, "上次确认同步", stamp, nil)
	})

	if data.SyncErr != nil {
		notice(c, "本机同步记录读取失败。刷新后重试，项目和本机连接仍可查看。")
	} else if m := syncProblem(data.LastOutcome); m != "" {
		notice(c, m)
	} else if len(data.Sync.Conflicts) > 0 {
		notice(c, fmt.Sprintf("%d 条记录存在冲突，可从“查看冲突”定位队列中的记录。", len(data.Sync.Conflicts)))
	}

	section(c, "本机绑定的项目")
	switch {
	case data.BindingsErr != nil:
		notice(c, "本机项目绑定读取失败，请刷新后重试。")
	case len(projects) == 0:
		secondary(c, "尚无绑定目录。可在项目目录运行 ctxferry project bind .，然后刷新。")
	default:
		d.projectTable(c, projects)
	}
	ui.Divider(c)
	section(c, "本机连接")
	d.connectionDetails(c)
}

func overviewProjects(data app.DesktopSnapshot) []overviewProject {
	indices := map[string]int{}
	var projects []overviewProject
	for _, b := range data.Bindings {
		i, ok := indices[b.ProjectID]
		if !ok {
			i = len(projects)
			indices[b.ProjectID] = i
			projects = append(projects, overviewProject{id: b.ProjectID, name: b.Project})
		}
		projects[i].paths = append(projects[i].paths, b.Path)
	}
	for _, r := range data.Queue {
		if i, ok := indices[r.ProjectID]; ok {
			if r.Waiting() {
				projects[i].pending++
			}
			if r.Conflict {
				projects[i].conflicts++
			}
		}
	}
	return projects
}

func (d *desktop) projectTable(c *ui.Context, rows []overviewProject) {
	s := &d.overviewState
	s.list.Selected = &s.selected
	s.list.RowHeight = 32
	s.list.Key = func(row int) any { return rows[row].id }
	s.list.Label = func(row int) string { return rows[row].name }
	ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Stretch).Children(func() {
		columns := []ui.TableColumn{{Title: "项目"}, {Title: "待发布队列", Width: 99}, {Title: "冲突", Width: 66}}
		ui.Table(c.Key("bound-projects"), &s.list, columns, len(rows), func(row, col int) {
			p := rows[row]
			pending, conflicts := "未知", "未知"
			if d.data.SyncErr == nil {
				pending, conflicts = strconv.Itoa(p.pending), strconv.Itoa(p.conflicts)
			}
			ui.Text(c, []string{p.name, pending, conflicts}[col]).NoWrap()
		}).Grow(1).Height(tableHeight(len(rows)))
		ui.Column(c).Width(235).Padding(10, 12).Gap(6).Radius(6).Background(panel).Children(func() {
			secondary(c, "所选项目")
			if s.selected < 0 || s.selected >= len(rows) {
				secondary(c, "选择一行查看本机目录")
				return
			}
			p := rows[s.selected]
			section(c, p.name)
			for _, path := range p.paths {
				ui.Text(c, path).FillWidth()
			}
			if d.data.SyncErr == nil && p.conflicts > 0 {
				d.queueLink(c, queueConflicts, p.id, "查看项目冲突", true)()
			}
			if desktopPages[pageProjects] != nil && ui.Button(c, "打开项目").Clicked() {
				d.navigate(desktopRoute{Page: pageProjects, ProjectID: p.id})
			}
		})
	})
}

func (d *desktop) queueLink(c *ui.Context, filter queueFilter, project, label string, visible bool) func() {
	if !visible {
		return nil
	}
	return func() {
		if ui.Button(c, label).Clicked() {
			d.syncState.setFilter(filter)
			d.navigate(desktopRoute{Page: pageSync, ProjectID: project})
		}
	}
}

func (d *desktop) connectionDetails(c *ui.Context) {
	s := d.data
	switch {
	case s.SyncErr != nil:
		detail(c, "Git 认证", "未知 · 无法读取本机同步记录")
	case s.Sync.Config == nil:
		detail(c, "Git 认证", "尚未配置同步仓库")
	default:
		git := "当前认证未经检查 · 可在同步页检查远端"
		switch s.LastOutcome {
		case "auth-required":
			git = "上次同步需要认证"
		case "unlock-required":
			git = "上次同步需要解锁"
		}
		if check := d.currentRemoteCheck(); check != nil {
			// A successful listing proves read access, not how Git authenticated.
			switch {
			case check.GitReachable:
				git = "远端读取成功 · 当前认证机制未经验证"
			case check.Failure == "auth-required":
				git = "远端检查需要认证"
			case check.Failure == "unlock-required":
				git = "远端检查需要解锁"
			default:
				git = "远端读取不可用 · 当前认证未经验证"
			}
		}
		detail(c, "Git 认证", git)
	}
	switch {
	case s.ProvidersErr != nil:
		detail(c, "Provider 认证", "未知 · 无法读取本机绑定")
	case len(s.Providers) == 0:
		detail(c, "Provider 认证", "已绑定项目没有引用凭据配置")
	default:
		for _, p := range s.Providers {
			detail(c, p.Profile.Provider, p.Profile.Name+" · "+providerStatus(p))
		}
	}
	status := map[string]string{
		"running": "用户服务运行中", "installed": "已安装，当前未运行",
		"foreground-only": "未安装用户服务", "unavailable": "用户服务管理器不可用",
	}
	detail(c, "后台同步", valueOr(status[s.BackgroundSync.Status], "未知"))
}

func providerStatus(p app.DoctorProvider) string {
	switch {
	case p.Headless == "scoped-runtime-session-configured":
		return "已配置本机运行会话 · 登录未经验证"
	case p.Status == "unavailable" && p.StoredSession == provider.CredentialAbsent && p.Headless == "not-configured":
		return "未保存会话 · 如需访问，请登录"
	case p.StoredSession == provider.CredentialPresent:
		return "本机有会话记录 · 登录未经验证"
	case p.Status == "unavailable":
		return "本机认证不可用 · 运行 ctxferry doctor 查看详情"
	default:
		return "本机登录状态未知"
	}
}
