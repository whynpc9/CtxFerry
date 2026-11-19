//go:build !linux && !darwin && !windows

package platform

import (
	"errors"
	"os"
)

func RenameSkillExclusive(*os.File, string, *os.File, string) error {
	return errors.New("exclusive skill projection is unsupported on this platform")
}

func SkillFileIdentity(string) (string, error) {
	return "", errors.New("skill file identity is unsupported on this platform")
}

func SyncSkillDirectory(string) error { return nil }

func ReplaceSkillRecord(source, destination string) error { return os.Rename(source, destination) }
