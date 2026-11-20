package main

import (
	"errors"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/store"
)

var (
	ink    = ui.Hex("#193a34")
	muted  = ui.Hex("#48605a")
	panel  = ui.Hex("#f1f5f2")
	line   = ui.Hex("#d7e2dc")
	accent = ui.Hex("#276550")
	theme  = ui.LightTheme().WithAccent(accent)
)

func section(c *ui.Context, s string)   { ui.Text(c, s).FontSize(17).Bold().TextColor(ink) }
func secondary(c *ui.Context, s string) { ui.Text(c, s).TextColor(muted) }
func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func detail(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(12).Padding(4, 0).Children(func() {
		ui.Text(c, label).Width(108).TextColor(muted)
		ui.Text(c, value).Grow(1).TextColor(ink)
	})
}
func notice(c *ui.Context, message string) {
	ui.Text(c, message).FillWidth().Padding(10, 12).Radius(5).Background(ui.Hex("#faf0e7")).TextColor(ui.Hex("#723b25"))
}

func (d *desktop) view(c *ui.Context) {
	c.SetTheme(theme)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(166).Padding(10, 10).Gap(8).Background(panel).Children(func() {
			ui.Text(c, "CtxFerry").FontSize(23).Bold().TextColor(ink).Padding(7, 8)
			ui.Divider(c)
			d.sidebar(c)
			if d.data.Device.ID != "" {
				ui.Divider(c)
				ui.Column(c).Padding(4, 8).Children(func() {
					ui.Text(c, d.data.Device.Name).Bold()
					secondary(c, "本机 · "+d.data.Device.OS+" · "+d.data.Device.Arch)
				})
			}
		})
		ui.Column(c).Grow(1).Children(func() {
			d.toolbar(c)
			ui.Divider(c)
			body := ui.Scroll(c.Key(d.route.Page)).Grow(1).FillWidth()
			switch d.route.Page {
			case pageSync:
				body.TrackScroll(&d.syncState.scroll)
			case pageConflict:
				if d.conflict != nil {
					body.TrackScroll(&d.conflict.scroll)
				}
			case pageSecrets:
				body.TrackScroll(&d.secrets.scroll)
			case pageFleet:
				body.TrackScroll(&d.fleet.scroll)
			}
			body.Children(func() {
				ui.Column(c).FillWidth().Padding(18, 22).Gap(12).Children(func() {
					if d.data.ReadAt.IsZero() {
						d.catalogState(c)
						return
					}
					if d.err != nil {
						notice(c, "刷新失败，正在显示上次读取的本机快照。请检查当前工作区后重试。")
					}
					if render := desktopPages[d.route.Page]; render != nil {
						render(d, c)
					}
				})
			})
		})
	})
}

func (d *desktop) toolbar(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(8, 20).Gap(10).Background(ui.Hex("#f8faf8")).Children(func() {
		if d.route.Page == pageConflict && d.conflict != nil {
			d.conflictToolbar(c)
			return
		}
		secondary(c, d.workspaceBreadcrumb())
		ui.Spacer(c)
		if !d.data.ReadAt.IsZero() {
			ui.Text(c, "本机快照 · "+d.data.ReadAt.Local().Format("2006-01-02 15:04:05")).FontSize(12).TextColor(muted)
		}
		refresh := "刷新本地视图"
		if d.loading {
			refresh = "正在刷新…"
		}
		if ui.Button(c.Key("refresh-local"), refresh).Disabled(d.loading || d.syncState.backingUp).Clicked() {
			d.refresh()
			if d.route.Page == pageSecrets || d.route.Page == pageFleet {
				d.refreshManagement()
			}
		}
		if d.route.Page == pageSync && d.data.SyncErr == nil && d.data.Sync.Config != nil {
			d.remoteButton(c)
		}
	})
}

func (d *desktop) catalogState(c *ui.Context) {
	switch {
	case d.loading:
		section(c, "正在读取本机快照…")
	case errors.Is(d.err, store.ErrNotInitialized):
		section(c, "尚未初始化工作区")
		ui.Text(c, "在终端运行 ctxferry init，然后刷新本地视图。")
	case errors.Is(d.err, store.ErrMigrationRequired):
		section(c, "本机目录需要升级")
		ui.Text(c, "使用匹配版本的 CLI 升级目录后刷新。")
	case errors.Is(d.err, store.ErrUnsupportedSchema):
		section(c, "本机目录来自较新版本")
		ui.Text(c, "更新 CtxFerry 后重试。")
	default:
		section(c, "本机快照不可用")
		ui.Text(c, "运行 ctxferry doctor 检查目录和数据库，然后刷新。")
	}
}

func pageHeading(c *ui.Context, title, subtitle string) {
	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, title).FontSize(27).Bold().TextColor(ink)
		secondary(c, subtitle)
	})
}

func metrics(c *ui.Context, children func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Padding(11, 13).Gap(10).
		Background(panel).Border(1, line).Radius(6).Children(children)
}

func metric(c *ui.Context, label, value string, action func()) {
	ui.Column(c).Basis(0).Grow(1).Gap(4).Children(func() {
		secondary(c, label)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, value).FontSize(17).Bold().TextColor(ink)
			if action != nil {
				action()
			}
		})
	})
}

func observedTime(stamp *string) string {
	if stamp == nil {
		return "尚无确认"
	}
	t, err := time.Parse(time.RFC3339Nano, *stamp)
	if err != nil {
		return "时间不可用"
	}
	t = t.Local()
	if t.Format(time.DateOnly) == time.Now().Format(time.DateOnly) {
		return "今天 " + t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}
