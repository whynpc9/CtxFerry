// Package provider defines the boundary between catalog references, device
// authentication and external secret backends.
package provider

import (
	"context"
	"errors"
	"time"
)

type Profile struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Method   string `json:"method"`
}

// Locator identifies a stable entry ID or an exact backend namespace and key.
type Locator struct {
	EntryID     string `json:"entryId,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	Environment string `json:"environment,omitempty"`
	Path        string `json:"path,omitempty"`
	Key         string `json:"key,omitempty"`
}

// MethodCapabilities are reported per authentication method. NativeExec is
// informational: CtxFerry builds the child environment itself unless a native
// path can restrict the injected set and exclude authentication material.
type MethodCapabilities struct {
	Method        string `json:"method"`
	MachineAuth   bool   `json:"machineAuth"`
	OfflineRead   bool   `json:"offlineRead"`
	NativeExec    bool   `json:"nativeExec"`
	FileDelivery  bool   `json:"fileDelivery"`
	CreateImport  bool   `json:"createImport,omitempty"`
	SessionExpiry bool   `json:"sessionExpiry"`
}

type AuthState string

const (
	Authenticated AuthState = "authenticated"
	SignedOut     AuthState = "signed-out"
	Locked        AuthState = "locked"
	Expired       AuthState = "expired"
)

type Authentication struct {
	State     AuthState  `json:"state"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

var (
	ErrAuthenticationRequired = errors.New("provider authentication required")
	ErrSecretUnavailable      = errors.New("provider entry unavailable")
	ErrNoSession              = errors.New("no device session")
)

// Adapter keeps authentication material and values inside the provider boundary.
// Login returns an opaque session for CredentialStore. Adapters must not persist
// it themselves. Resolve must use the exact Locator and distinguish an invalid
// session from an absent or denied entry. Login and Resolve return caller-owned
// byte slices.
type Adapter interface {
	Capabilities() []MethodCapabilities
	Login(context.Context, Profile) ([]byte, error)
	Authentication(context.Context, Profile, []byte) (Authentication, error)
	Resolve(context.Context, Profile, []byte, Locator) ([]byte, error)
}

// Importer is optional and must be advertised for the selected method. Import
// creates one exact shared entry and must never overwrite or adopt an entry.
// The value and session are borrowed for the duration of the call.
type Importer interface {
	Import(context.Context, Profile, []byte, Locator, []byte) error
}

var (
	ErrImportCollision = errors.New("provider entry already exists")
	ErrImportRejected  = errors.New("provider rejected the import")
	ErrImportPending   = errors.New("provider approval is pending")
	ErrImportUnknown   = errors.New("provider import outcome is unknown")
)

type SessionKey struct {
	WorkspaceID string
	DeviceID    string
	Profile     Profile
}

// CredentialStore is implemented by secure device credential storage. A nil
// implementation or a failed availability check disables login and session use.
// Load returns a caller-owned copy. Save must not retain the caller's slice.
type CredentialStore interface {
	Available(context.Context) error
	Load(context.Context, SessionKey) ([]byte, error)
	Save(context.Context, SessionKey, []byte) error
}

type Services struct {
	Adapters    map[string]Adapter
	Credentials CredentialStore
	// InjectedSession reads an explicitly provisioned runtime session for the
	// exact device profile. It returns a caller-owned copy, or nil if absent.
	InjectedSession func(SessionKey) []byte
	// HasInjectedSession checks only whether the scoped injection is configured.
	HasInjectedSession func(SessionKey) bool
}

// CredentialEvidence is metadata about the store and an exact session entry.
// An entry's presence does not establish that its session is usable.
type CredentialEvidence string

const (
	CredentialUnknown     CredentialEvidence = "unknown"
	CredentialAvailable   CredentialEvidence = "available"
	CredentialUnavailable CredentialEvidence = "unavailable"
	CredentialLocked      CredentialEvidence = "locked"
	CredentialPresent     CredentialEvidence = "present"
	CredentialAbsent      CredentialEvidence = "absent"
)

// CredentialInspector is optional. Inspection must not unlock the store,
// retrieve values, start a service, or write a probe entry.
type CredentialInspector interface {
	InspectCredentials(context.Context, []SessionKey) (CredentialEvidence, string, map[SessionKey]CredentialEvidence)
}
