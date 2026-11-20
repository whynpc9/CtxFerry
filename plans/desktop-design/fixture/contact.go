package main

import (
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// writeContactSheets lays the rendered frames out nine to a sheet, in
// reading order, with MyGo's own Image widget and renderer.
func writeContactSheets(fs []frame, framesDir, outDir string) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	const per, cols, tileW, tileH = 9, 3, 600, 400 // tileH fits both 1.6 and 1.51 aspect frames
	for start, sheet := 0, 1; start < len(fs); start, sheet = start+per, sheet+1 {
		end := min(start+per, len(fs))
		group := fs[start:end]
		bms := make([]*ui.Bitmap, len(group))
		for i, f := range group {
			data, err := os.ReadFile(filepath.Join(framesDir, fmt.Sprintf("%s-%s.png", f.id, f.slug)))
			if err != nil {
				log.Fatal(err)
			}
			if bms[i], err = ui.DecodeBitmapScaled(data, 1600); err != nil {
				log.Fatal(err)
			}
		}
		view := func(c *ui.Context) {
			c.SetTheme(theme())
			ui.Column(c).Fill().Padding(sp6).Gap(sp4).Background(tk.Canvas).Children(func() {
				ui.Text(c, fmt.Sprintf("CtxFerry desktop redesign · round 10 · frames %s–%s", group[0].id, group[len(group)-1].id)).FontSize(18).FontWeight(650)
				ui.Grid(c).Columns(cols).GapX(sp6).GapY(sp5).Children(func() {
					for i, f := range group {
						ui.Column(c).Gap(sp2).Children(func() {
							fw, fh := f.size()
							ui.Image(c, bms[i]).Width(tileW).Height(float32(tileW*fh/fw)).Radius(6).Border(1, tk.BorderStrong)
							ui.Text(c, f.id+" · "+f.title).FontSize(fsBody).FontWeight(600).SingleLine()
						})
					}
				})
			})
		}
		rows := (len(group) + cols - 1) / cols
		w := sp6*2 + cols*tileW + (cols-1)*sp6
		h := sp6*2 + 28 + sp4 + rows*(tileH+sp2+20) + (rows-1)*sp5
		img := ui.Render(view, w, h, 1)
		path := filepath.Join(outDir, fmt.Sprintf("sheet-%d.png", sheet))
		file, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			log.Fatal(err)
		}
		file.Close()
		fmt.Printf("%s  frames %s–%s\n", path, group[0].id, group[len(group)-1].id)
	}
}
