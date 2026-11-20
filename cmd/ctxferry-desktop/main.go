package main

import (
	"context"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/platform"
)

type desktop struct {
	core    app.App
	window  *mygo.Window
	ctx     context.Context
	page    string
	loading bool
	data    app.DesktopSnapshot
	err     error
}

func main() {
	dir, err := platform.DataDir("", false)
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("CtxFerry")
	mygo.App.WhenReady(func() {
		ctx, cancel := context.WithCancel(context.Background())
		d := &desktop{core: app.App{DataDir: dir}, ctx: ctx, page: "设备总览"}
		d.window = mygo.NewWindow(mygo.WindowOptions{Title: "CtxFerry", Width: 1180, Height: 750, MinWidth: 780, MinHeight: 480, StateKey: "workspace", Content: ui.View(d.view)})
		d.window.OnClosed(cancel)
		d.refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (d *desktop) refresh() {
	if d.loading {
		return
	}
	d.loading = true
	go func() {
		snapshot, err := d.core.ReadDesktopSnapshot(d.ctx)
		if d.ctx.Err() != nil {
			return
		}
		d.window.Update(func() {
			if d.ctx.Err() == nil {
				d.data, d.err, d.loading = snapshot, err, false
			}
		})
	}()
}
