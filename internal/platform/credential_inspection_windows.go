package platform

import (
	"context"
	"unsafe"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"golang.org/x/sys/windows"
)

func (systemCredentials) InspectCredentials(_ context.Context, _ []provider.SessionKey) (provider.CredentialEvidence, string, map[provider.SessionKey]provider.CredentialEvidence) {
	// Credential Manager's entry APIs return blobs. Query only the persistence
	// capability of this logon session, which contains no entry data.
	proc := windows.NewLazySystemDLL("advapi32.dll").NewProc("CredGetSessionTypes")
	if proc.Find() != nil {
		return provider.CredentialUnknown, "Cannot inspect Windows Credential Manager", nil
	}
	const generic = 1      // CRED_TYPE_GENERIC
	const localMachine = 2 // CRED_PERSIST_LOCAL_MACHINE, used by go-keyring
	var persist [generic + 1]uint32
	ok, _, err := proc.Call(uintptr(len(persist)), uintptr(unsafe.Pointer(&persist[0])))
	if ok == 0 {
		if err == windows.ERROR_NO_SUCH_LOGON_SESSION {
			return provider.CredentialUnavailable, "No Windows credential set for this logon session", nil
		}
		return provider.CredentialUnknown, "Cannot inspect Windows credential persistence", nil
	}
	if persist[generic] < localMachine {
		return provider.CredentialUnavailable, "Windows logon session does not support persistent generic credentials", nil
	}
	return provider.CredentialAvailable, "Windows logon session supports persistent generic credentials", nil
}
