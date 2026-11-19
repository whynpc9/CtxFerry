package provider

import (
	"os"
	"strings"
)

// EnvironmentSession describes a runtime session provisioned by a protected
// launcher. The workspace, device and profile must all match before use.
type EnvironmentSession struct {
	Provider    string
	Method      string
	ScopePrefix string
	Variable    string
}

func (s EnvironmentSession) Load(key SessionKey) []byte {
	if !s.matches(key) {
		return nil
	}
	return []byte(os.Getenv(s.Variable))
}

// Present checks existence only. Doctor does not read or validate sessions.
func (s EnvironmentSession) Present(key SessionKey) bool {
	if !s.matches(key) {
		return false
	}
	_, present := os.LookupEnv(s.Variable)
	return present
}

func (s EnvironmentSession) matches(key SessionKey) bool {
	return key.Profile.Provider == s.Provider && key.Profile.Method == s.Method &&
		key.WorkspaceID != "" && key.DeviceID != "" && key.Profile.Name != "" &&
		key.WorkspaceID == os.Getenv(s.ScopePrefix+"WORKSPACE_ID") &&
		key.DeviceID == os.Getenv(s.ScopePrefix+"DEVICE_ID") &&
		key.Profile.Name == os.Getenv(s.ScopePrefix+"PROFILE")
}

// CLIEnvironment gives provider CLIs access to their usual local settings
// without forwarding unrelated authentication or behavior overrides.
func CLIEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
			"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS",
			"TMPDIR", "TMP", "TEMP":
			env = append(env, entry)
		}
	}
	return env
}
