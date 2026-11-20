package main

import (
	"context"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/platform"
)

type desktop struct {
	core          app.App
	window        *mygo.Window
	update        func(func())
	ctx           context.Context
	route         desktopRoute
	origins       []desktopRoute
	setup         setupState
	loading       bool
	reload        bool
	compact       bool
	data          app.DesktopSnapshot
	err           error
	copiedCommand string

	overviewState    overviewState
	project          projectPage
	syncState        syncPageState
	conflict         *conflictPage
	resolvedRevision string
	workProjects     workProjects
	catalogProject   string
	skills           skillsState
	artifacts        artifactsState

	managementGeneration uint64
	projects             desktopProjects
	secrets              secretsView
	fleet                fleetView
}

func main() {
	registerFonts()
	dir, err := platform.DataDir("", false)
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("CtxFerry")
	mygo.App.WhenReady(func() {
		ctx, cancel := context.WithCancel(context.Background())
		d := &desktop{core: app.App{DataDir: dir, Providers: platform.ProviderServices()}, ctx: ctx, route: desktopRoute{Page: pageOverview}}
		options := mygo.WindowOptions{Title: "CtxFerry", Width: 1280, Height: 800, MinWidth: 1024, MinHeight: 680, Content: ui.View(d.view)}
		if _, isolated := os.LookupEnv("CTXFERRY_DATA_DIR"); !isolated {
			options.StateKey = "workspace"
		}
		d.window = mygo.NewWindow(options)
		d.window.OnClosed(cancel)
		d.refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// Work leaves the render thread. Each call site owns its in-flight state and
// decides whether the result still belongs to the current view.
func runDesktop[T any](d *desktop, work func(context.Context) (T, error), apply func(T, error)) {
	update := d.update
	if update == nil {
		update = d.window.Update
	}
	go func() {
		result, err := work(d.ctx)
		if d.ctx.Err() == nil {
			update(func() {
				if d.ctx.Err() == nil {
					apply(result, err)
				}
			})
		}
	}()
}

func (d *desktop) refresh() {
	if d.loading {
		d.reload = true
		return
	}
	d.loading = true
	runDesktop(d, d.core.ReadDesktopSnapshot, func(snapshot app.DesktopSnapshot, err error) {
		if err == nil {
			if snapshot.Workspace.ID != d.data.Workspace.ID || snapshot.Device.ID != d.data.Device.ID {
				d.overviewState = overviewState{}
				d.syncState = syncPageState{}
				d.conflict, d.resolvedRevision = nil, ""
				if d.route.Page == pageConflict {
					d.route.Page = pageOverview
				}
				d.workProjects, d.skills, d.artifacts = workProjects{}, skillsState{}, artifactsState{}
				d.catalogProject, d.origins = "", nil
				d.route = desktopRoute{Page: d.route.Page}
				d.managementGeneration++
				d.projects = desktopProjects{}
				d.secrets = secretsView{}
				d.fleet = fleetView{}
			}
			d.data = snapshot
		}
		d.err, d.loading = err, false
		if err == nil {
			d.enterWorkPage(true)
		}
		if d.reload {
			d.reload = false
			d.refresh()
		}
	})
}
