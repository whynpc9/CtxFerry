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
		ui.Text(c, "正在读取本地项目…")
		return false
	}
	if d.projects.err != nil {
		ui.Text(c, "本地项目读取失败，请刷新本地视图。")
		return false
	}
	if len(d.projects.items) == 0 {
		ui.Text(c, "暂无已订阅项目。")
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
	parts := []string{"本地工作区"}
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

// Use stable IDs in the controller while showing names in the native picker.
func optionPicker(c *ui.Context, title, selected string, options []desktopOption, disabled bool, onChange func(string)) {
	labels := make([]string, len(options))
	counts := make(map[string]int, len(options))
	for _, o := range options {
		counts[o.label]++
	}
	current := "请选择"
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
		ui.Text(c, title).TextColor(muted)
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
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(main)
		ui.Column(c).Width(310).Gap(10).Children(inspector)
	})
}

func inspectorPanel(c *ui.Context, content func()) {
	ui.Column(c).FillWidth().Background(panel).Radius(7).Padding(14).Gap(8).Children(content)
}

func labelValue(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(muted)
		ui.Text(c, value).FillWidth().TextColor(ink)
	})
}

func inspectorField(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, label).Width(91).FontSize(13).TextColor(muted)
		ui.Text(c, value).Grow(1).MinWidth(0).TextColor(ink)
	})
}

func timeLabel(timestamp string) string {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "尚未观察"
	}
	if t.Local().Format(time.DateOnly) == time.Now().Format(time.DateOnly) {
		return t.Local().Format("今天 15:04:05")
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func managementError(code string) string {
	switch code {
	case "binding-missing":
		return "槽位缺少有效的共享凭据映射。"
	case "binding-conflict":
		return "共享配置存在冲突，请先到同步页处理。"
	case "incompatible":
		return "要求、资源或共享引用不兼容。"
	case "authentication-required":
		return "请在当前执行环境完成 provider 登录或解锁，再更新本机会话。"
	case "credential-storage-unavailable":
		return "当前执行环境的安全凭据存储不可用。"
	case "secret-unavailable":
		return "无法确认该引用条目可读取。"
	case "provider-link-unavailable":
		return "无法核对 provider 管理地址，请在 provider 中按条目定位信息查找。"
	case "invalid-secret-binding", "usage":
		return "请核对别名、认证方式和条目定位字段。"
	case "selection-changed":
		return "所选配置已改变，请刷新并重新确认。"
	case "profile-not-found", "profile-required":
		return "配置方案不可用，请刷新后重新选择。"
	case "task-not-found":
		return "任务已不可用，请刷新后重新选择。"
	case "store-busy":
		return "本机数据库正忙，请重试。"
	default:
		return "操作未完成，请刷新本地记录后重试。"
	}
}

func showManagementError(c *ui.Context, code string) {
	if code != "" {
		ui.Text(c, managementError(code)+" ["+code+"]").FillWidth().TextColor(ui.Hex("#834706"))
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
		return env.Device.Name + " · " + env.Kind + "（本机）"
	case env.Local:
		return env.Device.Name + "（本机）"
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
				kind = "设备"
			}
		}
		return kind + " · " + id
	}
}

func environmentSystem(os, arch string) string {
	if os == "" {
		return "环境未知"
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
