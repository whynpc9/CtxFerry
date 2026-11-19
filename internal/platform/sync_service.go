package platform

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const foregroundSync = "Sync runs only in the foreground (ctxferry sync serve) or on invocation (ctxferry sync)."

type SyncService struct {
	Manager   string `json:"manager,omitempty"`
	Available bool   `json:"available"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Message   string `json:"message"`
}

type userSyncService struct {
	manager, tool, target, id string
	path, sid                 string
}

func serviceFor(dataDir string) userSyncService {
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}
	if runtime.GOOS == "windows" {
		dataDir = strings.ToLower(dataDir)
	}
	id := fmt.Sprintf("ctxferry-sync-%x", sha256.Sum256([]byte(dataDir)))[:len("ctxferry-sync-")+24]
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		config, _ := os.UserConfigDir()
		s := userSyncService{manager: "systemd", tool: "systemctl", id: id + ".service"}
		if filepath.IsAbs(config) {
			s.path = filepath.Join(config, "systemd", "user", s.id)
		}
		return s
	case "darwin":
		domain := "gui/" + strconv.Itoa(os.Getuid())
		s := userSyncService{manager: "launchd", tool: "/bin/launchctl", target: domain, id: "com.ctxferry." + id}
		if filepath.IsAbs(home) {
			s.path = filepath.Join(home, "Library", "LaunchAgents", s.id+".plist")
		}
		return s
	case "windows":
		sid := currentUserSID()
		return userSyncService{manager: "task-scheduler", tool: "schtasks.exe", id: id + "-" + sid, sid: sid}
	default:
		return userSyncService{}
	}
}

func (s userSyncService) command(ctx context.Context, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.tool, args...).Output()
	return string(out), err == nil
}

func (s userSyncService) run(ctx context.Context, args ...string) bool {
	_, ok := s.command(ctx, args...)
	return ok
}

func (s userSyncService) available(ctx context.Context) bool {
	switch s.manager {
	case "systemd":
		return s.path != "" && s.run(ctx, "--user", "show", "--property=Version")
	case "launchd":
		return s.path != "" && s.run(ctx, "print-disabled", s.target)
	case "task-scheduler":
		return s.sid != "" && s.run(ctx, "/Query", "/FO", "CSV", "/NH")
	default:
		return false
	}
}

// SyncServiceStatus queries the current user's manager. It never starts a job.
func SyncServiceStatus(ctx context.Context, dataDir string) SyncService {
	return serviceFor(dataDir).status(ctx)
}

func (s userSyncService) status(ctx context.Context) SyncService {
	result := SyncService{Manager: s.manager, Message: "No usable user service manager. " + foregroundSync}
	result.Available = s.available(ctx)
	if !result.Available {
		return result
	}
	switch s.manager {
	case "systemd":
		result.Installed = s.run(ctx, "--user", "is-enabled", s.id)
		result.Running = s.run(ctx, "--user", "is-active", s.id)
	case "launchd":
		_, err := os.Stat(s.path)
		result.Installed = err == nil
		if output, ok := s.command(ctx, "print", s.target+"/"+s.id); ok {
			for _, line := range strings.Split(output, "\n") {
				if strings.TrimSpace(line) == "state = running" {
					result.Running = true
					break
				}
			}
		}
	case "task-scheduler":
		result.Installed, result.Running = s.windowsState(ctx)
	}
	switch {
	case result.Running:
		result.Message = "Background sync is running in this user session."
	case result.Installed:
		result.Message = "Background sync is installed and currently stopped. " + foregroundSync
	default:
		result.Message = foregroundSync
	}
	return result
}

func InstallSyncService(ctx context.Context, dataDir string) (SyncService, error) {
	service := serviceFor(dataDir)
	if !service.available(ctx) {
		return SyncService{Manager: service.manager, Message: "No usable user service manager. " + foregroundSync}, ctx.Err()
	}
	exe, err := os.Executable()
	if err != nil {
		return SyncService{}, err
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return SyncService{}, err
	}
	args := []string{"sync", "serve", "--data-dir", dataDir}
	activated, err := service.install(ctx, exe, args)
	if err != nil {
		return SyncService{}, err
	}
	result := service.status(ctx)
	if !activated && result.Available {
		result.Message = "Could not activate the user sync service. " + result.Message
	}
	return result, ctx.Err()
}

func (s userSyncService) install(ctx context.Context, exe string, args []string) (bool, error) {
	switch s.manager {
	case "systemd":
		command := make([]string, 0, len(args)+1)
		for _, arg := range append([]string{exe}, args...) {
			command = append(command, strings.ReplaceAll(systemdQuote(arg), "$", "$$"))
		}
		unit := "[Unit]\nDescription=CtxFerry background sync\n\n[Service]\nType=exec\nExecStart=" + strings.Join(command, " ") +
			"\nEnvironment=" + systemdQuote("PATH="+servicePath()) +
			"\nRestart=on-failure\nRestartSec=5s\n\n[Install]\nWantedBy=default.target\n"
		if err := writeServiceFile(s.path, unit); err != nil {
			return false, err
		}
		return s.run(ctx, "--user", "daemon-reload") &&
			s.run(ctx, "--user", "enable", s.id) && s.run(ctx, "--user", "restart", s.id), nil
	case "launchd":
		var arguments strings.Builder
		for _, arg := range append([]string{exe}, args...) {
			arguments.WriteString("<string>" + xmlText(arg) + "</string>")
		}
		plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + s.id + `</string>
<key>ProgramArguments</key><array>` + arguments.String() + `</array>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>` + xmlText(servicePath()) + `</string></dict>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
</dict></plist>
`
		if err := writeServiceFile(s.path, plist); err != nil {
			return false, err
		}
		target := s.target + "/" + s.id
		if s.run(ctx, "print", target) && !s.run(ctx, "bootout", target) {
			return false, nil
		}
		return s.run(ctx, "enable", target) && s.run(ctx, "bootstrap", s.target, s.path), nil
	case "task-scheduler":
		return s.installTask(ctx, exe, args)
	}
	return false, nil
}

// Install only the executable search path, excluding working-directory entries.
func servicePath() string {
	var paths []string
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(path) {
			paths = append(paths, path)
		}
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

func systemdQuote(value string) string {
	return strconv.Quote(strings.ReplaceAll(value, "%", "%%"))
}

func xmlText(value string) string {
	var out strings.Builder
	xml.EscapeText(&out, []byte(value))
	return out.String()
}

func writeServiceFile(path, contents string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".ctxferry-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(contents)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
