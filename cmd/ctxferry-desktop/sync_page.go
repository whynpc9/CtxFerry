package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type queueFilter int

const (
	queueAll queueFilter = iota
	queuePending
	queueConflicts
)

type syncPageState struct {
	filter       int
	list         ui.ListState
	selected     int
	returnRoute  desktopRoute
	copiedRecord string

	checking   bool
	remote     remoteObservation
	remoteErr  bool
	browserErr bool

	backupOpen   bool
	backupPath   string
	includeLocal bool
	backingUp    bool
	backup       app.WorkspaceBackupResult
	backupAt     time.Time
	backupErr    string
}

type remoteObservation struct {
	config app.SyncConfig
	check  app.SyncCheck
	at     time.Time
}

func (s *syncPageState) setFilter(filter queueFilter) {
	if s.filter != int(filter) {
		s.filter = int(filter)
		s.list = ui.ListState{}
		s.selected = 0
	}
}

func (d *desktop) syncPage(c *ui.Context) {
	s := d.data.Sync
	subtitle := "尚未配置同步仓库"
	if s.Config != nil && d.data.SyncErr == nil {
		subtitle = "分支 " + s.Config.Branch + " · " + syncState(s.State)
	}
	if d.data.SyncErr != nil {
		subtitle = "本机同步记录不可用"
	}
	pageHeading(c, "同步", subtitle)
	if d.data.SyncErr != nil {
		notice(c, "无法读取本机同步记录。请运行 ctxferry doctor，再刷新本地视图。")
	} else {
		metrics(c, func() {
			metric(c, "待发布队列", strconv.Itoa(d.data.QueuePending), nil)
			metric(c, "未解决冲突", strconv.Itoa(len(s.Conflicts)), nil)
			metric(c, "上次确认同步", observedTime(s.LastSyncAt), nil)
		})
		if m := syncProblem(d.data.LastOutcome); m != "" {
			notice(c, m)
		}
		d.queue(c)
	}
	ui.Divider(c)
	d.repository(c)
	d.backupDialog(c)
}

func (d *desktop) queue(c *ui.Context) {
	state := &d.syncState
	previous := state.filter
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		section(c, "记录队列")
		ui.Spacer(c)
		ui.Segmented(c.Key("queue-filter"), &state.filter, "全部", "待发布", "冲突")
	})
	if previous != state.filter {
		filter := state.filter
		state.filter = previous
		state.setFilter(queueFilter(filter))
	}
	if d.route.ProjectID != "" {
		name := d.route.ProjectID
		for _, row := range d.data.Queue {
			if row.ProjectID == d.route.ProjectID {
				name = row.Project
				break
			}
		}
		ui.Row(c).Gap(8).Children(func() {
			secondary(c, "项目 "+name)
			if ui.Button(c, "清除项目筛选").Clicked() {
				d.route.ProjectID = ""
				state.list, state.selected = ui.ListState{}, 0
			}
		})
	}
	var rows []app.DesktopQueueRecord
	for _, r := range d.data.Queue {
		if d.route.ProjectID != "" && r.ProjectID != d.route.ProjectID {
			continue
		}
		switch queueFilter(state.filter) {
		case queuePending:
			if !r.Waiting() {
				continue
			}
		case queueConflicts:
			if !r.Conflict {
				continue
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		switch {
		case len(d.data.Queue) == 0:
			secondary(c, "本机队列为空，当前没有冲突。")
		case state.filter == int(queueConflicts):
			secondary(c, "当前筛选没有冲突记录。")
		default:
			secondary(c, "当前筛选没有匹配的记录。")
		}
		return
	}
	state.list.Selected = &state.selected
	state.list.RowHeight = 32
	state.list.Key = func(row int) any { return rows[row].ID }
	state.list.Label = func(row int) string { return recordName(rows[row]) }
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(14).Children(func() {
		cols := []ui.TableColumn{{Title: "记录"}, {Title: "项目", Width: 116}, {Title: "状态", Width: 82}}
		ui.Table(c.Key("sync-queue"), &state.list, cols, len(rows), func(row, col int) {
			r := rows[row]
			ui.Text(c, []string{recordName(r), valueOr(r.Project, "—"), recordState(r)}[col]).NoWrap()
		}).Grow(1).Height(tableHeight(len(rows)))
		ui.Column(c).Width(235).Padding(10, 12).Gap(5).Radius(6).Background(panel).Children(func() {
			secondary(c, "所选记录")
			if state.selected < 0 || state.selected >= len(rows) {
				secondary(c, "选择一行查看记录")
				return
			}
			r := rows[state.selected]
			section(c, recordName(r))
			secondary(c, recordState(r)+" · "+valueOr(r.Project, "项目未知"))
			ui.Text(c, r.ID).Font("monospace").FontSize(12).FillWidth()
			if r.Conflict {
				if desktopPages[pageConflict] != nil {
					if ui.PrimaryButton(c, "比较冲突版本").Clicked() {
						d.navigate(desktopRoute{Page: pageConflict, ProjectID: r.ProjectID, RecordID: r.ID})
					}
				} else if id, err := uuid.Parse(r.ID); err == nil && id.String() == r.ID {
					label := "复制比较命令"
					if state.copiedRecord == r.ID {
						label = "比较命令已复制"
					}
					if ui.Button(c, label).Tooltip("在终端查看此记录的三方版本").Clicked() {
						mygo.Clipboard.WriteText("ctxferry sync conflicts " + id.String())
						state.copiedRecord = r.ID
					}
				}
			}
		})
	})
}

func tableHeight(rows int) float32 { return float32(min(rows, 6)*33 + 33) }

func recordName(r app.DesktopQueueRecord) string { return valueOr(r.Title, r.ID) }
func recordState(r app.DesktopQueueRecord) string {
	switch {
	case r.Conflict:
		return "冲突"
	case r.Blocked:
		return "已阻塞"
	default:
		return "待发布"
	}
}

func syncState(state string) string {
	return valueOr(map[string]string{
		"enabled": "等待首次发布", "pending": "有待处理变更", "synced": "已有确认版本",
		"conflict": "存在未解决冲突", "offline": "上次同步远端不可用", "auth-required": "上次同步需要认证",
		"unlock-required": "上次同步需要解锁", "history-rewritten": "远端历史已改写",
		"incomplete": "同步数据不完整", "unsupported-schema": "远端格式不受支持",
		"privacy-unconfirmed": "远端私有性未确认", "missing-branch": "远端分支缺失",
	}[state], "本机同步记录")
}

func syncProblem(outcome string) string {
	return map[string]string{
		"offline":             "上次同步时远端不可用。下面仍显示本机记录和最后确认的版本。",
		"auth-required":       "上次同步需要 Git 或 GitHub 认证。更新本机登录后，可重新检查远端。",
		"unlock-required":     "上次同步需要解锁本机凭据助手或 SSH agent。",
		"history-rewritten":   "远端历史已改写，安全同步已暂停。请运行 ctxferry sync status 查看恢复信息。",
		"incomplete":          "上次同步的远端数据不完整。请检查远端，再核对本机保留的记录。",
		"unsupported-schema":  "远端使用当前版本无法读取的格式。请升级 CtxFerry。",
		"privacy-unconfirmed": "上次同步无法确认远端仓库为私有。请核对仓库，再重新检查远端。",
		"missing-branch":      "上次同步未找到配置的远端分支。请检查远端分支。",
	}[outcome]
}

func (d *desktop) repository(c *ui.Context) {
	s, state := d.data.Sync, &d.syncState
	ui.Row(c).FillWidth().Gap(10).Children(func() {
		section(c, "仓库与备份")
		ui.Spacer(c)
		if ui.Button(c, "创建备份…").Disabled(d.err != nil || d.loading || state.backingUp).Clicked() {
			state.backupErr = ""
			state.backupOpen = true
		}
	})

	// A write receipt or error belongs with the action that produced it.
	if state.backingUp || !state.backupAt.IsZero() || state.backupErr != "" {
		d.backupStatus(c)
	}

	switch {
	case d.data.SyncErr != nil:
		detail(c, "仓库", "未知 · 本机同步记录不可用")
	case s.Config == nil:
		secondary(c, "首次发布前需核验仓库为私有。发布后，共享正文会以明文写入 Git 历史。")
		detail(c, "远端确认", "不适用 · 尚未配置仓库")
		section(c, "连接私有仓库")
		ui.Text(c, "在终端用专用私有 GitHub 仓库的地址替换示例，配置后刷新：")
		command(c, "ctxferry sync enable --remote git@github.com:OWNER/REPO.git")
		firstPublishHelp(c)
	default:
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Column(c).Grow(1).Children(func() { detail(c, "仓库", s.Config.Remote) })
			if ui.Button(c, "在浏览器打开").Disabled(d.err != nil || d.loading).Clicked() {
				state.browserErr = mygo.Shell.OpenExternal(d.data.RepositoryURL) != nil
			}
		})
		if state.browserErr {
			notice(c, "无法打开系统浏览器。")
		}
		detail(c, "分支", s.Config.Branch)
		detail(c, "确认的 head", valueOr(s.Head, "尚无确认"))
		history := "尚未测量"
		if s.History != nil {
			history = historySize(s.History.RawBytes)
		}
		detail(c, "Git 历史体积", history)
		if h := s.History; h != nil && h.Warning != "" {
			if h.AutomaticPayloadPause {
				notice(c, "Git 历史体积已达到上限，自动发布新的产物内容已暂停。")
			} else {
				notice(c, "Git 历史体积较大，已发布的明文仍保留在历史中。")
			}
		}
		detail(c, "明文边界", "共享记录和正文以明文保留在 Git 历史中。仅本机记录、绑定目录、路由和 TCP 观察、凭据与认证不上传。")
		secondary(c, "发布前检查正文是否粘贴了敏感信息。")
		d.remoteStatus(c)
		if s.Head == "" {
			firstPublishHelp(c)
		}
	}
	if !state.backingUp && state.backupAt.IsZero() && state.backupErr == "" {
		d.backupStatus(c)
	}
}

func historySize(bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f MiB (%d B)", float64(bytes)/(1<<20), bytes)
}

func command(c *ui.Context, text string) { ui.Text(c, text).Font("monospace").FillWidth() }
func firstPublishHelp(c *ui.Context) {
	section(c, "首次发布")
	secondary(c, "启用同步后，在终端预览将发布的记录和对象。")
	command(c, "ctxferry sync preview --format json")
	secondary(c, "核对范围后，用返回的 previewId 替换 PREVIEW_ID。")
	command(c, "ctxferry sync init --preview PREVIEW_ID")
}

func (d *desktop) remoteButton(c *ui.Context) {
	state := &d.syncState
	label := "检查远端"
	if state.checking {
		label = "正在检查…"
	}
	if !ui.Button(c.Key("check-remote"), label).Disabled(d.err != nil || d.loading || state.checking).Clicked() {
		return
	}
	state.checking, state.remoteErr, state.remote = true, false, remoteObservation{}
	workspace := d.data.Workspace.ID
	runDesktop(d, func(ctx context.Context) (remoteObservation, error) {
		config, check, err := d.core.CheckDesktopRemote(ctx)
		return remoteObservation{config, check, time.Now()}, err
	}, func(result remoteObservation, err error) {
		if workspace != d.data.Workspace.ID {
			return
		}
		current := &d.syncState
		current.checking = false
		if err != nil {
			current.remoteErr = true
			return
		}
		current.remote = result
	})
}

func (d *desktop) currentRemoteCheck() *app.SyncCheck {
	s := d.data.Sync
	remote := &d.syncState.remote
	if d.data.SyncErr != nil || s.Config == nil || remote.at.IsZero() || *s.Config != remote.config {
		return nil
	}
	return &remote.check
}

func (d *desktop) remoteStatus(c *ui.Context) {
	if d.syncState.remoteErr {
		notice(c, "远端检查未完成。请运行 ctxferry sync check 查看本机 Git 配置，刷新后重试。")
	}
	check := d.currentRemoteCheck()
	if check == nil {
		detail(c, "远端检查", "尚未检查 · 可联网读取")
		return
	}
	git := "远端读取失败"
	if check.GitReachable {
		git = "远端读取成功"
	}
	privacy := valueOr(map[string]string{
		"private": "已核验私有仓库", "public": "公开仓库 · 发布受阻",
	}[check.Privacy], "私有性未确认")
	detail(c, "远端检查", git+" · "+privacy+" · "+d.syncState.remote.at.Format("15:04:05"))
	if check.Failure != "" {
		message := valueOr(map[string]string{
			"offline":             "无法连接 GitHub 检查私有性。",
			"git-error":           "Git 读取失败。请检查本机网络和 Git 认证后重试。",
			"auth-required":       "当前检查需要 Git 或 GitHub 认证。",
			"unlock-required":     "当前检查需要解锁 Git 凭据助手或 SSH agent。",
			"privacy-unconfirmed": "当前远端无法通过私有性检查。",
		}[check.Failure], "远端检查未全部通过。")
		notice(c, message)
	}
}
