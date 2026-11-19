package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/store"
)

var ink = ui.Hex("#193a34")
var muted = ui.Hex("#48605a")
var panel = ui.Hex("#eff5f1")
var theme = ui.LightTheme().WithAccent(ui.Hex("#276550"))

func section(c *ui.Context, s string) { ui.Text(c, s).FontSize(18).Bold().TextColor(ink) }
func detail(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Gap(12).Padding(5, 0).Children(func() {
		ui.Text(c, label).Width(108).TextColor(muted)
		ui.Text(c, value).Grow(1).TextColor(ink)
	})
}

func (d *desktop) view(c *ui.Context) {
	c.SetTheme(theme)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(178).Gap(14).Padding(16, 12).Background(panel).Children(func() {
			ui.Text(c, "CtxFerry").FontSize(23).Bold().TextColor(ink)
			ui.Divider(c)
			ui.Sidebar(c, &d.page, func() {
				for _, p := range []string{"设备总览", "同步"} {
					ui.SidebarItem(c, p, nil, p)
				}
			}).Grow(1)
			if d.data.Device.Name != "" {
				ui.Divider(c)
				ui.Text(c, d.data.Device.Name).Bold()
				ui.Text(c, d.data.Device.OS+" · "+d.data.Device.Arch).TextColor(muted)
			}
		})
		ui.Column(c).Grow(1).Children(func() {
			ui.Row(c).FillWidth().Padding(10, 20).Children(func() {
				ui.Text(c, "本地工作区").TextColor(muted)
				ui.Spacer(c)
				if !d.data.ReadAt.IsZero() && d.err == nil {
					ui.Text(c, "本机快照读取于 "+d.data.ReadAt.Local().Format("2006-01-02 15:04:05")).Margin(0, 12, 0, 0).TextColor(muted)
				}
				if ui.Button(c, "刷新本地视图").Disabled(d.loading).Clicked() {
					d.refresh()
				}
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Padding(20, 24).Gap(10).Children(func() {
					ui.Text(c, d.page).FontSize(27).Bold().TextColor(ink)
					switch {
					case d.loading:
						ui.Text(c, "正在读取本机快照…")
					case errors.Is(d.err, store.ErrNotInitialized):
						section(c, "尚未初始化工作区")
						ui.Text(c, "请先运行 ctxferry init，再刷新本地视图。")
					case errors.Is(d.err, store.ErrMigrationRequired):
						section(c, "本机目录需要升级")
						ui.Text(c, "使用匹配版本的 CLI 升级目录后刷新。")
					case d.err != nil:
						section(c, "本机快照不可用")
						ui.Text(c, "请使用 ctxferry doctor 检查目录和数据库状态，然后重试。")
					case d.page == "同步":
						d.syncPage(c)
					default:
						d.overview(c)
					}
				})
			})
		})
	})
}

func (d *desktop) overview(c *ui.Context) {
	s := d.data.Sync
	detail(c, "当前设备", d.data.Device.Name+" · "+d.data.Device.OS+" · "+d.data.Device.Arch)
	section(c, "本机绑定的项目目录")
	if len(d.data.Bindings) == 0 {
		ui.Text(c, "尚无绑定目录")
	} else {
		for _, b := range d.data.Bindings {
			detail(c, b.Project, b.Path)
		}
	}
	ui.Divider(c)
	section(c, "本机同步状态")
	if s.Config == nil {
		ui.Text(c, "尚未配置同步仓库")
	}
	detail(c, "本机队列记录", strconv.Itoa(s.Pending))
	detail(c, "本机未解决冲突", strconv.Itoa(len(s.Conflicts)))
	git := "本次刷新未检查远端认证"
	if s.Config == nil {
		git = "不适用（尚未配置远端）"
	}
	switch s.State {
	case "auth-required":
		git = "上次同步需要认证"
	case "unlock-required":
		git = "上次同步需要解锁"
	}
	detail(c, "Git 认证", git)
	detail(c, "凭据服务认证", "桌面快照未查询 Infisical 或 Bitwarden 登录状态")
}
func (d *desktop) syncPage(c *ui.Context) {
	s := d.data.Sync
	if s.Config == nil {
		ui.Column(c).FillWidth().Gap(7).Padding(16).Radius(6).Background(panel).Children(func() {
			section(c, "尚未配置同步仓库")
			ui.Text(c, "首次发布前需核验仓库为私有。发布后，共享正文将以明文写入 Git 历史。")
			detail(c, "远端确认", "不适用（无目标分支、确认版本或时间）")
		})
		d.queue(c)
		ui.Divider(c)
		section(c, "连接私有仓库")
		ui.Text(c, "准备好专用的私有 GitHub 仓库后，在终端替换仓库地址并运行：")
		ui.Text(c, "ctxferry sync enable --remote git@github.com:OWNER/REPO.git").Font("monospace").TextColor(ink)
		ui.Text(c, "配置成功后刷新本地视图。").TextColor(muted)
		firstPublishHelp(c)
		return
	}
	ui.Text(c, "发布后，共享正文将以明文保留在远端 Git 历史中。").TextColor(ink)
	section(c, "仓库与备份")
	detail(c, "仓库", s.Config.Remote)
	detail(c, "分支", s.Config.Branch)
	detail(c, "本地记录状态", s.State)
	head := "尚无确认记录"
	if s.Head != "" {
		head = s.Head
	}
	detail(c, "确认的 head", head)
	stamp := "尚无成功记录"
	if s.LastSyncAt != nil {
		stamp = *s.LastSyncAt
	}
	detail(c, "上次同步", stamp)
	size := "尚未测量"
	if s.History != nil {
		size = fmt.Sprintf("%d bytes", s.History.RawBytes)
	}
	detail(c, "Git 历史体积", size)
	detail(c, "最近备份", "本机目录未提供备份回执")
	if s.Head == "" && (s.State == "enabled" || s.State == "pending") {
		firstPublishHelp(c)
	}
	ui.Divider(c)
	d.queue(c)
}

func (d *desktop) queue(c *ui.Context) {
	s := d.data.Sync
	section(c, "本机记录队列")
	detail(c, "队列记录", strconv.Itoa(s.Pending))
	for _, id := range d.data.PendingIDs {
		detail(c, "待发布", id)
	}
	detail(c, "未解决冲突", strconv.Itoa(len(s.Conflicts)))
	for _, record := range s.Conflicts {
		detail(c, "冲突", record.ID)
	}
	for _, item := range s.Blocked {
		detail(c, "已阻塞", item.ID+" · "+item.Reason)
	}
}

func firstPublishHelp(c *ui.Context) {
	section(c, "首次发布")
	ui.Text(c, "启用同步后，在终端预览将发布的记录和对象：")
	ui.Text(c, "ctxferry sync preview --format json").Font("monospace").TextColor(ink)
	ui.Text(c, "核对范围后，用返回的 previewId 替换 PREVIEW_ID，执行首次发布：")
	ui.Text(c, "ctxferry sync init --preview PREVIEW_ID").Font("monospace").TextColor(ink)
}
