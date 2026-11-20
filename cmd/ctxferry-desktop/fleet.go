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
			if d.route.Page == pageFleet && d.route.RecordID == resource &&
				d.validProject(valueOr(d.route.ProjectID, f.project)) == project {
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
	pageHeading(c, "设备与资源", "按执行环境查看服务路线和观察结果")
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
	metrics(c, func() {
		optionPicker(c, "项目", f.project, d.projectOptions(), f.wait, func(id string) {
			d.navigate(desktopRoute{Page: pageFleet, ProjectID: id})
			d.loadFleet(id, "")
		})
		optionPicker(c, "资源", f.resource, resources, f.wait, func(id string) {
			d.navigate(desktopRoute{Page: pageFleet, ProjectID: f.project, RecordID: id})
			d.loadFleet(f.project, id)
		})
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, "业务阶段").TextColor(muted)
			ui.Text(c, valueOr(f.data.Selected.Stage, "—")).Bold()
		})
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, "执行环境").TextColor(muted)
			if f.issue == "" && len(f.data.Devices) > 0 {
				ui.Text(c, fmt.Sprintf("%d 个", len(f.data.Devices))).Bold()
			} else {
				ui.Text(c, "—")
			}
		})
	})
	showManagementError(c, f.issue)
	if f.wait {
		ui.Text(c, "正在处理当前资源请求…").TextColor(muted)
	}
	if f.receipt != nil {
		o := f.receipt
		ui.Text(c, "已记录本机观察 · "+observationLabel(o)+" · "+o.Scope.Target+" · "+timeLabel(o.ObservedAt)).
			FillWidth().Padding(10, 12).Background(panel).Radius(6).TextColor(ink)
	}
	if f.issue != "" || len(f.data.Devices) == 0 {
		return
	}
	if len(f.data.Resources) == 0 {
		ui.Text(c, "此项目暂无资源配置。")
	}
	managementColumns(c, func() { d.fleetTable(c) }, func() { d.fleetInspector(c) })
}

func (d *desktop) fleetTable(c *ui.Context) {
	f := &d.fleet
	section(c, "执行环境")
	const rowHeight = 36
	f.devices.RowHeight = rowHeight
	f.devices.Selected = &f.selected
	f.devices.Key = func(i int) any { return f.data.Devices[i].Device.ID }
	f.devices.Label = func(i int) string { return environmentLabel(f.data.Devices[i]) }
	ui.Table(c, &f.devices, []ui.TableColumn{
		{Title: "执行环境"}, {Title: "系统", Width: 122}, {Title: "观察结果", Width: 124}, {Title: "观察时间", Width: 152},
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
	if f.selected < 0 || f.selected >= len(f.data.Devices) {
		f.selected = 0
	}
	env := f.data.Devices[f.selected]
	resource := f.data.Selected
	if resource.ID == "" {
		return
	}
	ui.Divider(c)
	section(c, "访问路线")
	labelValue(c, "资源 · 用途", resource.Identity+" · "+resource.Purpose)
	labelValue(c, "配置端点", valueOr(strings.Join(resource.Endpoints, " · "), "无共享端点"))
	applicability := valueOr(strings.Join(resource.Platforms, " · "), "所有平台")
	labelValue(c, "适用平台 · 所需网络", applicability+" / "+valueOr(resource.Network, "不限网络"))
	if env.Local {
		route := f.data.Route
		labelValue(c, "本机路线", routeLabel(route))
	} else {
		labelValue(c, "该设备路线", "未知")
	}
	ui.Divider(c)
	section(c, "最近观察")
	if len(env.Observations) == 0 {
		ui.Text(c, "该执行环境暂无此资源的观察。")
	}
	for _, o := range env.Observations {
		qualifier := ""
		if o.Scope.ResourceRevision != resource.Revision {
			qualifier = " · 旧资源版本"
		} else if env.Local && f.data.Probe != nil && o.Scope != f.data.Probe.Scope {
			qualifier = " · 旧路线"
		}
		labelValue(c, timeLabel(o.ObservedAt)+" · "+o.Tool+qualifier, observationLabel(&o)+" · "+o.Scope.Target+observationNetwork(o))
	}
}

func (d *desktop) fleetInspector(c *ui.Context) {
	f := &d.fleet
	env := f.data.Devices[f.selected]
	inspectorPanel(c, func() {
		ui.Text(c, "所选执行环境").TextColor(muted)
		section(c, environmentLabel(env))
		statusLabel(c, f.latestLabel(env))
		labelValue(c, "设备 ID", env.Device.ID)
		inspectorField(c, "系统", environmentSystem(env.Device.OS, env.Device.Arch))
		if !env.Local {
			labelValue(c, "Provider 认证", "此设备的会话状态未知")
			ui.Text(c, "请在该执行环境内登录或探测。").FillWidth().TextColor(muted)
			return
		}
		labelValue(c, "Provider 认证", "按凭据绑定中的本机会话单独检查")
		if f.data.Selected.ID == "" {
			return
		}
		ui.Divider(c)
		if f.data.Probe == nil {
			labelValue(c, "探测不可用", routeLabel(f.data.Route))
			showManagementError(c, f.probeIssue)
			return
		}
		probe := *f.data.Probe
		labelValue(c, probeTargetLabel(probe.Scope), probe.Scope.Target)
		showManagementError(c, f.probeIssue)
		if ui.PrimaryButton(c, "在本机探测 TCP…").Disabled(f.wait).FillWidth().Clicked() {
			f.confirm = true
		}
		if f.confirm {
			target := fmt.Sprintf("%s 将对 %s 发起一次 TCP 连接。结果写入当前环境的观察记录。", env.Device.Name, probe.Scope.Target)
			if probe.Scope.Kind == "proxy-endpoint" {
				target += "\n\n此探测只验证代理 TCP 端口。"
			}
			if ui.AlertDialog(c, &f.confirm, "确认本机 TCP 探测", target, "取消探测", "开始探测") == 1 {
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
	if o != nil && (o.Scope.ResourceRevision != f.data.Selected.Revision ||
		env.Local && (f.data.Probe == nil || f.data.Probe.Scope != o.Scope)) {
		return "旧观察"
	}
	return observationLabel(o)
}

func observationLabel(o *app.Observation) string {
	if o == nil || !slices.Contains([]string{"reachable", "unreachable"}, o.Status) || o.Tool != "tcp-connect" {
		return "未知"
	}
	label := "TCP 可达"
	if o.Status == "unreachable" {
		label = "TCP 不可达"
	}
	if o.Scope.Kind == "proxy-endpoint" {
		label = "代理 " + label
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
		return "探测目标 · 代理端口"
	}
	return "探测目标 · 服务端口"
}

func routeLabel(r app.ResourceRoute) string {
	switch r.Status {
	case "resolved":
		source := "共享资源"
		if r.Source == "device-local" {
			source = "本机配置"
		}
		path := r.Endpoint
		if r.Proxy != "" {
			path = r.Proxy + " → " + path
		}
		return path + " · " + source
	case "platform-unsupported":
		return "资源不适用于当前平台"
	case "network-mismatch":
		return "当前环境未匹配所需网络"
	case "route-ambiguous":
		return "有多个候选端点，请先配置本机路线"
	case "route-missing":
		return "尚无本机可用端点"
	default:
		return "未知"
	}
}
