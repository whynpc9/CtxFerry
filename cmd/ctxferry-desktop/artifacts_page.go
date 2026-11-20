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
	allRuns    = "All runs"
	allDevices = "All source devices"
	allContent = "All content states"
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
	displayLimit          int
}

func (d *desktop) setArtifactProject(project string) {
	a := &d.artifacts
	if a.busy || a.project == project && a.loaded {
		return
	}
	a.project, a.run, a.device, a.content = project, allRuns, allDevices, allContent
	d.route.ProjectID = project
	a.row, a.table, a.showRun, a.displayLimit = -1, ui.ListState{}, false, 5
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
		if a.displayLimit == 0 {
			a.displayLimit = 5
		}
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

func (a *artifactsState) shownArtifacts() []app.DesktopArtifact {
	limit := a.displayLimit
	if limit <= 0 {
		limit = 5
	}
	return a.visible[:min(limit, len(a.visible))]
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
	card(c).Children(func() {
		ui.Row(c).FillWidth().Gap(sp3).Wrap().Children(func() {
			d.projectPicker(c, a.project, false, a.loading || a.busy, d.setArtifactProject)
			workPicker(c, "Run", &a.run, runs, a.project == "" || a.loading || a.busy, a.filterArtifacts)
			workPicker(c, "Source device", &a.device, devices, a.project == "" || a.loading || a.busy, a.filterArtifacts)
			workPicker(c, "Content state", &a.content, contents, a.project == "" || a.loading || a.busy, a.filterArtifacts)
		})
	})
	switch {
	case a.loading || d.workProjects.loading:
		muted(c, "Reading the artifact catalog…")
		return
	case a.err != nil:
		workMessage(c, "The artifact catalog could not be read", a.err)
		return
	case a.project == "":
		muted(c, "No subscribed Project is available for this scope.")
		return
	case len(a.items) == 0:
		card(c).Children(func() { muted(c, "This Project has no registered run artifacts.") })
		return
	case len(a.visible) == 0:
		card(c).Children(func() {
			muted(c, "No artifacts match the current filters.")
			ui.Button(c, "Clear filters").OnClick(func() {
				a.run, a.device, a.content = allRuns, allDevices, allContent
				a.filterArtifacts()
			})
		})
		return
	}
	muted(c, plural(len(a.visible), "artifact", "artifacts")+" · newest first")
	managementColumns(c, func() {
		card(c).Padding(0).Clip().Children(func() {
			a.artifactTable(c)
			shown := len(a.shownArtifacts())
			listFooter(c, fmt.Sprintf("Showing 1–%d of %d · newest first", shown, len(a.visible)), func() {
				if shown < len(a.visible) {
					ui.Button(c, fmt.Sprintf("Load %d older", len(a.visible)-shown)).OnClick(func() { a.displayLimit = len(a.visible) })
				}
			})
		})
		if a.row >= 0 && a.row < len(a.visible) {
			d.artifactRun(c, a.visible[a.row])
		}
	}, func() {
		if a.row >= 0 && a.row < len(a.visible) {
			d.artifactInspector(c, a.visible[a.row])
		}
	})
}

func (a *artifactsState) artifactTable(c *ui.Context) {
	columns := []ui.TableColumn{{Title: "File"}, {Title: "Size", Width: 82}, {Title: "Content", Width: 150}}
	a.table.Selected = &a.row
	shown := a.shownArtifacts()
	a.table.Key = func(row int) any { return shown[row].ID }
	a.table.Label = func(row int) string { return shown[row].Payload.Name }
	ui.Table(c, &a.table, columns, len(shown), func(row, col int) {
		item := shown[row]
		content := artifactContentLabel(item.Content)
		if mark := a.checked[item.ID]; mark != "" && mark != "Local content verified" {
			content = mark
		}
		values := [3]string{item.Payload.Name, artifactSize(item.Payload.Size), content}
		ui.Text(c, values[col]).NoWrap().Ellipsis("…").Tooltip(values[col])
	}).FillWidth().Height(float32(min(9, len(shown))+1)*31 + 10).Disabled(a.busy).OnChange(func() {
		a.actionErr, a.showRun = nil, false
	})
}

func (d *desktop) artifactRun(c *ui.Context, a app.DesktopArtifact) {
	run := a.Run
	card(c).Children(func() {
		cardHeader(c, icActivity, "Run record", run.Summary, func() {
			if run.SourceRevision != "" {
				label := "Show provenance"
				if d.artifacts.showRun {
					label = "Hide provenance"
				}
				ui.Button(c, label).OnClick(func() { d.artifacts.showRun = !d.artifacts.showRun })
			}
		})
		device := run.DeviceID
		if device == d.data.Device.ID {
			device = d.data.Device.Name + " · current device"
		}
		kvGrid(c, []kvRow{
			{k: "Source device", v: device + " · " + run.OS + "/" + run.Arch},
			{k: "Run time", v: workTime(run.CreatedAt)},
			{k: "Run ID", v: run.ID, mono: true},
		})
		if !d.artifacts.showRun {
			return
		}
		workDetail(c, "Source revision", run.SourceRevision)
		if run.Dirty {
			workDetail(c, "Source state", "Local modifications · "+run.DiffID)
		}
		workDetail(c, "Profile", run.Profile)
		workDetail(c, "Exit code", fmt.Sprint(run.ExitCode))
		if len(run.ToolVersions) > 0 {
			workDetail(c, "Tool versions", strings.Join(run.ToolVersions, " · "))
		}
		if len(run.Inputs) > 0 {
			workDetail(c, "Inputs", strings.Join(run.Inputs, " · "))
		}
		if len(run.Uncompleted) > 0 {
			workDetail(c, "Uncompleted scope", strings.Join(run.Uncompleted, " · "))
		}
	})
}

func (d *desktop) artifactInspector(c *ui.Context, item app.DesktopArtifact) {
	a := &d.artifacts
	card(c).Children(func() {
		cardHeader(c, icPackage, item.Payload.Name, "Artifact metadata and content evidence", nil)
		label := artifactContentLabel(item.Content)
		if mark := a.checked[item.ID]; mark != "" {
			label = mark
		}
		pill(c, toneUnknown, label)
		completed := strings.Join(item.Run.Completed, " · ")
		if completed == "" {
			completed = "Run record does not state a completion scope"
			if item.Run.SourceRevision == "" {
				completed = "Synchronized record contains only a run summary"
			}
		}
		workDetail(c, "Completion scope", completed)
		size := artifactSize(item.Payload.Size)
		if item.Payload.Size >= 1024 {
			size += fmt.Sprintf(" · %d B", item.Payload.Size)
		}
		workDetail(c, "Original size", size)
		if s := item.Sync; s != nil {
			workDetail(c, "Synchronized content", artifactSyncLabel(s))
		}
		workDetail(c, "SHA-256", item.Payload.SHA256)
		if item.Location != "" {
			kind := "Local cache location"
			if item.Payload.Storage == "shared" {
				kind = "Synchronized snapshot object"
			}
			workDetail(c, kind, item.Location)
		} else if item.Content == "metadata-only" {
			workDetail(c, "Local content location", "Metadata only · locate the original bytes on the source device")
		}
		for _, obs := range item.Observations {
			if obs.DeviceID == d.data.Device.ID {
				workDetail(c, "Local sync verification", workTime(obs.ObservedAt))
				break
			}
		}
		if a.actionErr != nil {
			workMessage(c, "Content operation did not complete", a.actionErr)
		}
		if a.exported.ID == item.ID {
			banner(c, toneOk, "Bytes verified and exported", a.exported.Output, nil)
		}
		if item.Content == "cache" || item.Content == "snapshot" {
			ui.Button(c, "Verify local content").FillWidth().Disabled(a.busy).OnClick(func() { d.checkArtifact(item) })
			ui.PrimaryButton(c, "Verify and export…").FillWidth().Disabled(a.busy || d.window == nil).OnClick(func() { d.exportArtifact(item) })
		}
		if a.busy {
			muted(c, "Verifying local content…")
		}
	})
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
			Parent: window, Title: "Verify and export artifact", DefaultPath: item.Payload.Name, CreateDirectories: true,
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
		a.checked[id] = "Local content verified"
		return
	}
	switch app.Classify(err).Code {
	case "artifact-hash-mismatch", "artifact-invalid", "artifact-changed":
		a.checked[id] = "Integrity check failed"
	case "artifact-unavailable":
		a.checked[id] = "Local content is missing"
	}
}

func artifactRunLabel(run app.ArtifactRun) string {
	return run.Summary + " · " + run.ID
}

func artifactContentLabel(content string) string {
	switch content {
	case "cache":
		return "Local cache"
	case "snapshot":
		return "Snapshot exportable"
	case "missing":
		return "Local content missing"
	case "invalid":
		return "Content invalid"
	case "unavailable":
		return "Local content unreadable"
	default:
		return "Metadata only"
	}
}

func artifactSyncLabel(s *app.ArtifactSyncStatus) string {
	if s.Reason != "" {
		reason := map[string]string{
			"metadata-selected": "Metadata-only selected", "payload-size": "Payload exceeds sync size limit",
			"sensitive": "Sensitive content", "excluded-format": "Format excluded from sync", "history-pause": "Git history size limit",
			"payload-unavailable": "Payload unavailable at publication", "publication-budget": "Publication budget limit", "staging-budget": "Staging budget limit",
		}[s.Reason]
		if reason == "" {
			reason = s.Reason
		}
		if s.State == "pending" {
			return "Waiting · metadata only · " + reason
		}
		return "Metadata only · " + reason
	}
	switch s.State {
	case "shared":
		return "Metadata and bytes"
	case "pending":
		return "Waiting for publication"
	case "withdrawn":
		return "Withdrawn"
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
