//go:build desktop_smoke

package main

import (
	"os"

	"github.com/egoist/mygo"
)

func applyDesktopSmokeWindowOptions(options *mygo.WindowOptions) {
	if os.Getenv("CTXFERRY_DESKTOP_COMPACT") == "1" {
		options.Width, options.Height = compactW, compactH
	}
}
