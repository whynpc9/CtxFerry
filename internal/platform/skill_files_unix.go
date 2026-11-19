//go:build linux || darwin

package platform

import (
	"fmt"
	"os"
	"syscall"
)

// SkillFileIdentity identifies the directory entry itself, including a link.
func SkillFileIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

func SyncSkillDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ReplaceSkillRecord(source, destination string) error { return os.Rename(source, destination) }
