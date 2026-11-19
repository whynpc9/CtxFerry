package platform

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func currentUserSID() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Uid
}

func (s userSyncService) windowsState(ctx context.Context) (installed, running bool) {
	// Query the scheduler's numeric state so the result is independent of
	// the localized status strings printed by schtasks.exe.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	script := `$ErrorActionPreference = 'Stop'; $service = New-Object -ComObject 'Schedule.Service'; $service.Connect(); $task = $service.GetFolder('\').GetTask('` + s.id + `'); [int]$task.Enabled; [int]$task.State`
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	out, err := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) != 2 {
		return false, false
	}
	return fields[0] == "1", fields[1] == "4"
}

func (s userSyncService) installTask(ctx context.Context, exe string, args []string) (bool, error) {
	dir, err := os.MkdirTemp("", "ctxferry-task-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "sync.xml")
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	task := `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + xmlText(s.sid) + `</UserId></LogonTrigger></Triggers>
  <Principals><Principal id="User"><UserId>` + xmlText(s.sid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>
  </Settings>
  <Actions Context="User"><Exec><Command>` + xmlText(exe) + `</Command><Arguments>` + xmlText(strings.Join(quoted, " ")) + `</Arguments></Exec></Actions>
</Task>
`
	if err = os.WriteFile(path, []byte(task), 0600); err != nil {
		return false, err
	}
	return s.run(ctx, "/Create", "/TN", s.id, "/XML", path, "/F") && s.run(ctx, "/Run", "/TN", s.id), nil
}
