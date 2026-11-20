package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

const (
	allRuns    = "全部运行"
	allDevices = "全部设备"
	allContent = "全部状态"
)

type artifactsState struct {
	project               string
	run, device, content  string
	items, visible        []app.DesktopArtifact
	row                   int
	table                 ui.ListState
	loading, loaded, busy bool
	readID                uint64
	err, actionErr        error
	checked               map[string]string
	exported              app.ArtifactFetch
	showRun               bool
}

func (d *desktop) setArtifactProject(project string) {
	a := &d.artifacts
	if a.busy || a.project == project && a.loaded {
		return
	}
	a.project, a.run, a.device, a.content = project, allRuns, allDevices, allContent
	d.route.ProjectID = project
	a.row, a.table, a.showRun = -1, ui.ListState{}, false
	d.loadArtifacts()
}

func (d *desktop) loadArtifacts() {
	a := &d.artifacts
	project := a.project
	if project == "" {
		return
	}
	a.readID++
	readID := a.readID
	a.loading, a.err, a.actionErr = true, nil, nil
	desktopJob(d, func(ctx context.Context) ([]app.DesktopArtifact, error) {
		return d.core.ListDesktopArtifacts(ctx, project)
	}, func(items []app.DesktopArtifact, err error) {
		if a.readID != readID {
			return
		}
		a.loading, a.loaded, a.items, a.err, a.checked = false, true, items, err, nil
		a.filterArtifacts()
	})
}

func (a *artifactsState) filterArtifacts() {
	var selected string
	if a.row >= 0 && a.row < len(a.visible) {
		selected = a.visible[a.row].ID
	}
	a.visible = nil
	a.row = -1
	for _, item := range a.items {
		if a.run != allRuns && a.run != artifactRunLabel(item.Run) ||
			a.device != allDevices && a.device != item.Run.DeviceID ||
			a.content != allContent && a.content != artifactContentLabel(item.Content) {
			continue
		}
		if item.ID == selected {
			a.row = len(a.visible)
		}
		a.visible = append(a.visible, item)
	}
	if a.row == -1 && len(a.visible) > 0 {
		a.row = 0
	}
	a.table = ui.ListState{}
}

func (d *desktop) artifactsPage(c *ui.Context) {
	a := &d.artifacts
	runs, devices, contents := []string{allRuns}, []string{allDevices}, []string{allContent}
	for _, item := range a.items {
		runs = append(runs, artifactRunLabel(item.Run))
		devices = append(devices, item.Run.DeviceID)
		contents = append(contents, artifactContentLabel(item.Content))
	}
	for _, list := range [][]string{runs, devices, contents} {
		slices.Sort(list[1:])
	}
	runs, devices, contents = slices.Compact(runs), slices.Compact(devices), slices.Compact(contents)
	ui.Row(c).FillWidth().Gap(12).Wrap().Padding(12, 14).Radius(7).Background(panel).Children(func() {
		d.projectPicker(c, a.project, false, a.loading || a.busy, d.setArtifactProject)
		workPicker(c, "运行", &a.run, runs, a.project == "" || a.loading || a.busy, a.filterArtifacts)
		workPicker(c, "来源设备", &a.device, devices, a.project == "" || a.loading || a.busy, a.filterArtifacts)
		workPicker(c, "内容状态", &a.content, contents, a.project == "" || a.loading || a.busy, a.filterArtifacts)
	})
	switch {
	case a.loading || d.workProjects.loading:
		ui.Text(c, "正在读取产物目录…").TextColor(muted)
		return
	case a.err != nil:
		workMessage(c, "产物目录读取失败", a.err)
		return
	case a.project == "":
		ui.Text(c, "工作区还没有可选项目。").TextColor(muted)
		return
	case len(a.items) == 0:
		ui.Text(c, "这个项目还没有登记运行产物。").TextColor(muted)
		return
	case len(a.visible) == 0:
		ui.Text(c, "当前筛选没有匹配的产物。").TextColor(muted)
		ui.Button(c, "清除筛选").OnClick(func() {
			a.run, a.device, a.content = allRuns, allDevices, allContent
			a.filterArtifacts()
		})
		return
	}
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
			section(c, fmt.Sprintf("运行产物 · %d 项", len(a.visible)))
			a.artifactTable(c)
			if a.row >= 0 && a.row < len(a.visible) {
				ui.Divider(c)
				d.artifactRun(c, a.visible[a.row])
			}
		})
		workInspector(c, func() {
			if a.row >= 0 && a.row < len(a.visible) {
				d.artifactInspector(c, a.visible[a.row])
			}
		})
	})
}

func (a *artifactsState) artifactTable(c *ui.Context) {
	columns := []ui.TableColumn{{Title: "文件"}, {Title: "大小", Width: 74}, {Title: "内容状态", Width: 126}}
	a.table.Selected = &a.row
	a.table.Key = func(row int) any { return a.visible[row].ID }
	a.table.Label = func(row int) string { return a.visible[row].Payload.Name }
	ui.Table(c, &a.table, columns, len(a.visible), func(row, col int) {
		item := a.visible[row]
		content := artifactContentLabel(item.Content)
		if mark := a.checked[item.ID]; mark != "" && mark != "本机内容已校验" {
			content = mark
		}
		values := [3]string{item.Payload.Name, artifactSize(item.Payload.Size), content}
		ui.Text(c, values[col]).NoWrap().Ellipsis("…")
	}).FillWidth().Height(float32(min(9, len(a.visible))+1)*31 + 10).Disabled(a.busy).OnChange(func() {
		a.actionErr, a.showRun = nil, false
	})
}

func (d *desktop) artifactRun(c *ui.Context, a app.DesktopArtifact) {
	run := a.Run
	ui.Row(c).FillWidth().Gap(10).Wrap().Children(func() {
		section(c, "运行记录")
		if run.SourceRevision != "" {
			ui.Spacer(c)
			label := "展开追溯信息"
			if d.artifacts.showRun {
				label = "收起追溯信息"
			}
			ui.Button(c, label).OnClick(func() { d.artifacts.showRun = !d.artifacts.showRun })
		}
	})
	workDetail(c, "摘要", run.Summary)
	device := run.DeviceID
	if device == d.data.Device.ID {
		device = d.data.Device.Name + " · 当前设备"
	}
	workDetail(c, "来源设备", device+" · "+run.OS+"/"+run.Arch)
	workDetail(c, "运行时间", workTime(run.CreatedAt))
	workDetail(c, "运行 ID", run.ID)
	if !d.artifacts.showRun {
		return
	}
	workDetail(c, "源修订", run.SourceRevision)
	if run.Dirty {
		workDetail(c, "源状态", "有本地修改 "+run.DiffID)
	}
	workDetail(c, "配置方案", run.Profile)
	workDetail(c, "退出码", fmt.Sprint(run.ExitCode))
	if len(run.ToolVersions) > 0 {
		workDetail(c, "工具版本", strings.Join(run.ToolVersions, " · "))
	}
	if len(run.Inputs) > 0 {
		workDetail(c, "输入", strings.Join(run.Inputs, " · "))
	}
	if len(run.Uncompleted) > 0 {
		workDetail(c, "未完成范围", strings.Join(run.Uncompleted, " · "))
	}
}

func (d *desktop) artifactInspector(c *ui.Context, item app.DesktopArtifact) {
	a := &d.artifacts
	section(c, item.Payload.Name)
	label := artifactContentLabel(item.Content)
	if mark := a.checked[item.ID]; mark != "" {
		label = mark
	}
	ui.Text(c, label).Bold().Padding(5, 8).Radius(5).Background(ui.Hex("#e1ebe5")).TextColor(ink)
	ui.Divider(c)
	completed := strings.Join(item.Run.Completed, " · ")
	if completed == "" {
		completed = "运行记录未注明"
		if item.Run.SourceRevision == "" {
			completed = "同步记录只含运行摘要"
		}
	}
	workDetail(c, "完成范围", completed)
	size := artifactSize(item.Payload.Size)
	if item.Payload.Size >= 1024 {
		size += fmt.Sprintf(" · %d B", item.Payload.Size)
	}
	workDetail(c, "原始大小", size)
	if s := item.Sync; s != nil {
		workDetail(c, "同步内容", artifactSyncLabel(s))
	}
	workDetail(c, "SHA-256", item.Payload.SHA256)
	if item.Location != "" {
		kind := "本机缓存位置"
		if item.Payload.Storage == "shared" {
			kind = "同步快照对象"
		}
		workDetail(c, kind, item.Location)
	} else if item.Content == "metadata-only" {
		workDetail(c, "本机内容位置", "仅保留元数据")
	}
	for _, obs := range item.Observations {
		if obs.DeviceID == d.data.Device.ID {
			workDetail(c, "本机同步校验", workTime(obs.ObservedAt))
			break
		}
	}
	if a.actionErr != nil {
		workMessage(c, "内容操作未完成", a.actionErr)
	}
	if a.exported.ID == item.ID {
		workDetail(c, "已校验并导出", a.exported.Output)
	}
	if item.Content == "cache" || item.Content == "snapshot" {
		ui.Button(c, "校验本机内容").FillWidth().Disabled(a.busy).OnClick(func() { d.checkArtifact(item) })
		ui.PrimaryButton(c, "校验并导出…").FillWidth().Disabled(a.busy || d.window == nil).OnClick(func() { d.exportArtifact(item) })
	}
	if a.busy {
		ui.Text(c, "正在处理本机内容…").TextColor(muted)
	}
}

func (d *desktop) checkArtifact(item app.DesktopArtifact) {
	a := &d.artifacts
	project := a.project
	a.busy, a.actionErr = true, nil
	desktopJob(d, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, d.core.CheckDesktopArtifact(ctx, project, item.ID)
	}, func(_ struct{}, err error) {
		a.busy, a.actionErr = false, err
		a.recordArtifactCheck(item.ID, err)
		d.enterWorkPage(false)
	})
}

func (d *desktop) exportArtifact(item app.DesktopArtifact) {
	a := &d.artifacts
	project, dir, window := a.project, d.core.DataDir, d.window
	a.busy, a.actionErr = true, nil
	desktopJob(d, func(ctx context.Context) (app.ArtifactFetch, error) {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: window, Title: "校验并导出产物", DefaultPath: item.Payload.Name, CreateDirectories: true,
		})
		if err != nil || path == "" {
			return app.ArtifactFetch{}, err
		}
		if !filepath.IsAbs(path) {
			return app.ArtifactFetch{}, app.Fail("invalid-path", "select an absolute output path")
		}
		return d.core.FetchArtifact(ctx, project, dir, item.ID, path)
	}, func(result app.ArtifactFetch, err error) {
		a.busy, a.actionErr = false, err
		if result.ID != "" {
			a.exported = result
			a.recordArtifactCheck(item.ID, nil)
		} else if err != nil {
			a.recordArtifactCheck(item.ID, err)
		}
		d.enterWorkPage(false)
	})
}

func (a *artifactsState) recordArtifactCheck(id string, err error) {
	if a.checked == nil {
		a.checked = map[string]string{}
	}
	if err == nil {
		a.checked[id] = "本机内容已校验"
		return
	}
	switch app.Classify(err).Code {
	case "artifact-hash-mismatch", "artifact-invalid", "artifact-changed":
		a.checked[id] = "完整性校验失败"
	case "artifact-unavailable":
		a.checked[id] = "本机内容已缺失"
	}
}

func artifactRunLabel(run app.ArtifactRun) string {
	return run.Summary + " · " + run.ID
}

func artifactContentLabel(content string) string {
	switch content {
	case "cache":
		return "本机缓存"
	case "snapshot":
		return "同步快照可导出"
	case "missing":
		return "本机内容缺失"
	case "invalid":
		return "内容异常"
	case "unavailable":
		return "本机内容不可读取"
	default:
		return "仅元数据"
	}
}

func artifactSyncLabel(s *app.ArtifactSyncStatus) string {
	if s.Reason != "" {
		reason := map[string]string{
			"metadata-selected": "选择仅同步元数据", "payload-size": "文件超过同步大小上限",
			"sensitive": "敏感内容", "excluded-format": "文件类型不纳入同步", "history-pause": "Git 历史体积限制",
			"payload-unavailable": "发布时内容不可用", "publication-budget": "发布额度限制", "staging-budget": "暂存额度限制",
		}[s.Reason]
		if reason == "" {
			reason = s.Reason
		}
		if s.State == "pending" {
			return "待发布 · 仅元数据 · " + reason
		}
		return "仅元数据 · " + reason
	}
	switch s.State {
	case "shared":
		return "元数据和文件"
	case "pending":
		return "待发布"
	case "withdrawn":
		return "已撤回"
	default:
		return s.State
	}
}

func artifactSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	scaled := float64(size) / 1024
	for _, unit := range units {
		if scaled < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", scaled, unit)
		}
		scaled /= 1024
	}
	return fmt.Sprintf("%d B", size)
}

func workTime(raw string) string {
	if stamp, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return stamp.Local().Format("2006-01-02 15:04:05")
	}
	return raw
}
