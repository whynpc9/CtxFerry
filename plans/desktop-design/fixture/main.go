// Command ctxferry-design-fixture renders the CtxFerry desktop redesign
// frames with MyGo Native UI. It uses only package ui widgets and MyGo's
// own renderer: no HTML, CSS, browser or web view.
//
//	go run . -out ../frames            # render every frame as PNG (offscreen)
//	go run . -out ../frames -only 08   # render one frame
//	go run . -check                    # report text laid out beyond the window
//	go run . -contrast                 # WCAG ratios of every text/surface token pair
//	go run . -list                     # print the frame inventory
//	go run . -window 08                # open one frame in a native MyGo window
//	go run . -out ../frames -contact ../contact-sheets  # 9-up overview sheets
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type frame struct {
	id    string // ordered number, "01"
	slug  string
	title string
	dest  string // primary destination
	state string // data state shown
	focus int    // Tab presses before capture, to show the keyboard focus ring
	w, h  int    // window size in DIP; 0 means the 1280x800 default
	view  func(c *ui.Context)
}

func (f frame) size() (int, int) {
	if f.w == 0 {
		return windowW, windowH
	}
	return f.w, f.h
}

func main() {
	out := flag.String("out", "../frames", "directory for PNG frames")
	only := flag.String("only", "", "render only the frame with this id")
	scale := flag.Float64("scale", 2, "device pixels per DIP")
	check := flag.Bool("check", false, "report text laid out beyond the window and exit")
	list := flag.Bool("list", false, "print the frame inventory and exit")
	window := flag.String("window", "", "open the frame with this id in a native window")
	contact := flag.String("contact", "", "write contact sheets of the PNGs in -out to this directory")
	audit := flag.Bool("audit", false, "report one-line text that is cut with an ellipsis in any frame and fail if any is found")
	contrastFlag := flag.Bool("contrast", false, "print WCAG contrast of every text/surface token pair and fail below 4.5:1")
	flag.Parse()

	registerFonts()
	fs := allFrames()

	switch {
	case *contrastFlag:
		if bad := runContrast(); bad > 0 {
			log.Fatalf("%d pairs below 4.5:1", bad)
		}
		return
	case *list:
		for _, f := range fs {
			fmt.Printf("%s  %-34s %-10s %s\n", f.id, f.slug, f.dest, f.state)
		}
		return
	case *window != "":
		openWindow(fs, *window)
		return
	case *contact != "":
		writeContactSheets(fs, *out, *contact)
		return
	case *audit:
		bad := 0
		for _, f := range fs {
			bad += auditFrame(f)
		}
		fmt.Printf("audit: %d frames, %d one-line texts cut with an ellipsis\n", len(fs), bad)
		if bad > 0 {
			os.Exit(1)
		}
		return
	case *check:
		bad := 0
		for _, f := range fs {
			bad += checkFrame(f)
		}
		if bad > 0 {
			log.Fatalf("%d text elements outside the window or sliced by the page edge", bad)
		}
		fmt.Printf("check: %d frames, no text laid out beyond its window (1280x800, compact %dx%d) and no line sliced by the page edge or a pinned bar\n", len(fs), compactW, compactH)
		return
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	type entry struct {
		ID, File, Title, Destination, State, SHA256 string
		Width, Height                               int
	}
	var manifest []entry
	for _, f := range fs {
		if *only != "" && f.id != *only {
			continue
		}
		fw, fh := f.size()
		tt := ui.NewTesterWith(f.view, fw, fh, ui.TesterOptions{})
		tt.SetScale(float32(*scale))
		for i := 0; i < f.focus; i++ {
			tt.Key(0, ui.KeyTab)
		}
		tt.Frame()
		img := tt.Image()
		name := fmt.Sprintf("%s-%s.png", f.id, f.slug)
		path := filepath.Join(*out, name)
		file, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			log.Fatal(err)
		}
		file.Close()
		data, _ := os.ReadFile(path)
		sum := sha256.Sum256(data)
		b := img.Bounds()
		manifest = append(manifest, entry{f.id, name, f.title, f.dest, f.state, hex.EncodeToString(sum[:]), b.Dx(), b.Dy()})
		fmt.Printf("%s  %dx%d  %s\n", name, b.Dx(), b.Dy(), hex.EncodeToString(sum[:])[:12])
	}
	if *only == "" {
		data, _ := json.MarshalIndent(manifest, "", "  ")
		if err := os.WriteFile(filepath.Join(*out, "manifest.json"), append(data, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// checkFrame lays a frame out headlessly and reports any text whose box
// reaches past the window: content that a reviewer would not see.
func checkFrame(f frame) int {
	fw, fh := f.size()
	tt := ui.NewTesterWith(f.view, fw, fh, ui.TesterOptions{})
	tt.Frame()
	bad := 0
	seen := map[string]bool{}
	var page ui.Rect
	if a, ok := tt.Access("Page content"); ok {
		page = a.Bounds
	}
	for _, s := range tt.Texts() {
		if seen[s] || strings.TrimSpace(s) == "" || s == "Page content" {
			continue
		}
		seen[s] = true
		r, ok := tt.Find(s)
		if !ok {
			continue
		}
		if r.Y+r.H > float32(fh)+0.5 || r.X+r.W > float32(fw)+0.5 {
			fmt.Printf("%s %s: %q at %.0f,%.0f %.0fx%.0f\n", f.id, f.slug, s, r.X, r.Y, r.W, r.H)
			bad++
		}
		// Find reports a text's visible box, clipped by the page's scroll
		// area. A line whose visible box ends on the page's bottom edge (the
		// window, or the pinned bar under the page) is cut through: it
		// shows as a sliced line in the default view.
		if bottom := page.Y + page.H; page.H > 0 && r.H > 0 && r.Y < bottom-1 && r.Y+r.H > bottom-0.75 {
			fmt.Printf("%s %s: %q sliced by the page edge at %.0f\n", f.id, f.slug, s, bottom)
			bad++
		}
	}
	return bad
}

func openWindow(fs []frame, id string) {
	var view func(c *ui.Context)
	w, h := windowW, windowH
	for _, f := range fs {
		if f.id == id {
			view = f.view
			w, h = f.size()
		}
	}
	if view == nil {
		log.Fatalf("no frame %q", id)
	}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "CtxFerry design fixture · frame " + id,
			Width:   w,
			Height:  h,
			Content: ui.View(view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// auditWrap turns every cut() text into wrapping text. The audit lays each
// frame out both ways: a one-line text whose box grows taller when it may
// wrap did not fit, so the normal frame shows it with an ellipsis.
var auditWrap bool

// cut keeps a text on one line with an ellipsis. Every one-line text in the
// fixture goes through it, so the audit can find each one that is cut.
func cut(e ui.Element) ui.Element {
	if auditWrap {
		return e
	}
	return e.SingleLine()
}

func textBoxes(f frame) map[string]ui.Rect {
	fw, fh := f.size()
	tt := ui.NewTesterWith(f.view, fw, fh, ui.TesterOptions{})
	tt.Frame()
	out := map[string]ui.Rect{}
	for _, s := range tt.Texts() {
		if _, ok := out[s]; ok || strings.TrimSpace(s) == "" {
			continue
		}
		if r, ok := tt.Find(s); ok {
			out[s] = r
		}
	}
	return out
}

func auditFrame(f frame) int {
	auditWrap = false
	normal := textBoxes(f)
	auditWrap = true
	wrapped := textBoxes(f)
	auditWrap = false
	bad := 0
	for s, r := range normal {
		w, ok := wrapped[s]
		if ok && w.H > r.H+2 {
			fmt.Printf("%s %s: cut %q (%.0f DIP wide)\n", f.id, f.slug, s, r.W)
			bad++
		}
	}
	return bad
}
