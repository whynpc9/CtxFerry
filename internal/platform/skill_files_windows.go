package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func RenameSkillExclusive(fromDir *os.File, from string, toDir *os.File, to string) error {
	return moveSkillFile(filepath.Join(fromDir.Name(), from), filepath.Join(toDir.Name(), to), windows.MOVEFILE_WRITE_THROUGH)
}

func ReplaceSkillRecord(source, destination string) error {
	return moveSkillFile(source, destination, windows.MOVEFILE_WRITE_THROUGH|windows.MOVEFILE_REPLACE_EXISTING)
}

func moveSkillFile(from, to string, flags uint32) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, dest, flags)
}

func SkillFileIdentity(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", &os.PathError{Op: "open skill entry", Path: path, Err: err}
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

// Windows uses the write-through move for projection. Directory handles do
// not support the same flush operation as on Unix.
func SyncSkillDirectory(string) error { return nil }
