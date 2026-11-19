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
	} else {
		detail(c, "待发布记录", strconv.Itoa(s.Pending))
		detail(c, "未解决冲突", strconv.Itoa(len(s.Conflicts)))
	}
	git := "尚未验证"
	switch s.State {
	case "auth-required":
		git = "上次同步需要认证"
	case "unlock-required":
		git = "上次同步需要解锁"
	}
	detail(c, "Git 认证", git)
	detail(c, "凭据服务认证", "当前快照未验证")
}
func (d *desktop) syncPage(c *ui.Context) {
	s := d.data.Sync
	ui.Text(c, "共享正文以明文保存在私有 Git 历史中。").TextColor(ink)
	if s.Config == nil {
		section(c, "尚未配置同步仓库")
		return
	}
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
	ui.Divider(c)
	section(c, "记录队列")
	detail(c, "待发布记录", strconv.Itoa(s.Pending))
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
