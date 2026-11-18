package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DataDir resolves the complete application root, independently of any checkout.
func DataDir(override string, explicit bool) (string, error) {
	if !explicit {
		override, explicit = os.LookupEnv("CTXFERRY_DATA_DIR")
	}
	if explicit {
		if override == "" {
			return "", fmt.Errorf("data directory override must not be empty")
		}
		return filepath.Abs(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is unset")
		}
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		if !filepath.IsAbs(base) {
			return "", fmt.Errorf("XDG_DATA_HOME must be absolute")
		}
	}
	return filepath.Join(base, "ctxferry"), nil
}
