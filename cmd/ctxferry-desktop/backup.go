package main

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func (d *desktop) backupDialog(c *ui.Context) {
	s := &d.syncState
	ui.DialogBase(c, &s.backupOpen, func(backdrop, dialog ui.Element) {
		backdrop.Background(ui.RGBA(16, 31, 26, .35))
		dialog.Width(520).Padding(22).Gap(11).Radius(8).Background(ui.Hex("#ffffff"))
		section(c, "创建工作区备份")
		ui.Text(c, "备份写入本机的新目录，默认包含共享记录和已确认的 Git 历史。").FillWidth()
		secondary(c, "备份是明文。请保护目标目录；凭据、登录会话和执行信任不纳入备份。")
		ui.Text(c, "目标目录（绝对路径）")
		ui.TextInput(c.Key("backup-destination"), &s.backupPath).FillWidth().Label("备份目标目录").
			Placeholder("输入尚不存在的目录").AutoFocus()
		ui.Checkbox(c, &s.includeLocal, "也包含仅本机数据及绑定目录信息")
		if s.backupErr != "" {
			notice(c, s.backupErr)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				s.backupOpen = false
			}
			path := strings.TrimSpace(s.backupPath)
			if ui.PrimaryButton(c, "确认创建备份").Disabled(d.err != nil || d.loading || s.backingUp || !filepath.IsAbs(path)).Clicked() {
				d.createBackup(path, s.includeLocal)
			}
		})
	})
}

func (d *desktop) createBackup(path string, includeLocal bool) {
	s := &d.syncState
	s.backingUp, s.backupErr, s.backupOpen = true, "", false
	runDesktop(d, func(ctx context.Context) (app.WorkspaceBackupResult, error) {
		return d.core.BackupWorkspace(ctx, path, includeLocal)
	}, func(result app.WorkspaceBackupResult, err error) {
		s.backingUp = false
		if err != nil {
			s.backupErr = valueOr(map[string]string{
				"destination-exists": "目标目录已存在。请选择一个新目录。",
				"push-pending":       "有尚未确认的发布。请先在终端完成同步，再创建备份。",
				"sync-busy":          "同步正在使用工作区，请稍后重试。",
			}[app.Classify(err).Code], "备份未完成。请检查目标目录和写入权限，再选择新目录重试。")
			return
		}
		if result.WorkspaceID != d.data.Workspace.ID {
			s.backupErr = "备份完成，工作区已更换。请刷新后核对目标目录。"
			d.refresh()
			return
		}
		s.backup, s.backupAt, s.backupPath, s.includeLocal = result, time.Now(), "", false
	})
}

func (d *desktop) backupStatus(c *ui.Context) {
	s := &d.syncState
	switch {
	case s.backingUp:
		detail(c, "工作区备份", "正在创建明文备份…")
	case s.backupAt.IsZero():
		detail(c, "工作区备份", "此窗口尚无备份回执 · 可创建本机备份")
	default:
		detail(c, "备份已创建", s.backupAt.Format("2006-01-02 15:04:05"))
		detail(c, "备份目录", s.backup.Destination)
		scope := "共享数据"
		if s.backup.IncludesLocalOnly {
			scope = "共享与仅本机数据"
		}
		detail(c, "备份范围", scope+" · "+valueOr(s.backup.Head, "无已确认的远端版本"))
	}
	if s.backupErr != "" && !s.backupOpen {
		notice(c, s.backupErr)
	}
}
