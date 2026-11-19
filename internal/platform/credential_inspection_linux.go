package platform

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

func (systemCredentials) InspectCredentials(ctx context.Context, keys []provider.SessionKey) (provider.CredentialEvidence, string, map[provider.SessionKey]provider.CredentialEvidence) {
	uid := strconv.Itoa(os.Getuid())
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address == "" || address == "autolaunch:" {
		address = "unix:path=/run/user/" + uid + "/bus"
	}
	// Connect to an existing local bus, with neither bus nor service activation.
	for entry := range strings.SplitSeq(address, ";") {
		if !strings.HasPrefix(entry, "unix:") {
			return provider.CredentialUnknown, "Session bus address is not local Unix D-Bus", nil
		}
	}
	bus, err := dbus.Connect(address, dbus.WithContext(ctx), dbus.WithAuth(dbus.AuthExternal(uid)))
	if err != nil {
		return provider.CredentialUnavailable, "Cannot connect to the local session bus", nil
	}
	defer bus.Close()

	const service = "org.freedesktop.secrets"
	const root = "/org/freedesktop/secrets"
	var running bool
	if err := bus.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, service).Store(&running); err != nil {
		return provider.CredentialUnknown, "Cannot determine whether Secret Service is running", nil
	}
	if !running {
		return provider.CredentialUnavailable, "Secret Service is not running on the session bus", nil
	}
	obj := bus.Object(service, root)
	const noStart = dbus.FlagNoAutoStart
	const collectionInterface = "org.freedesktop.Secret.Collection"
	const properties = "org.freedesktop.DBus.Properties.Get"
	var collections dbus.Variant
	if err := obj.CallWithContext(ctx, properties, noStart, "org.freedesktop.Secret.Service", "Collections").Store(&collections); err != nil {
		return provider.CredentialUnknown, "Cannot inspect Secret Service collections", nil
	}
	paths, ok := collections.Value().([]dbus.ObjectPath)
	if !ok {
		return provider.CredentialUnknown, "Secret Service returned invalid collection metadata", nil
	}
	var path dbus.ObjectPath
	for _, candidate := range paths {
		if candidate == root+"/collection/login" {
			path = candidate
			break
		}
	}
	if path == "" {
		if err := obj.CallWithContext(ctx, "org.freedesktop.Secret.Service.ReadAlias", noStart, "default").Store(&path); err != nil {
			return provider.CredentialUnknown, "Cannot inspect the default Secret Service collection", nil
		}
		if path == "/" {
			return provider.CredentialUnavailable, "Secret Service has no login or default collection", nil
		}
	}

	collection := bus.Object(service, path)
	var property dbus.Variant
	if err := collection.CallWithContext(ctx, properties, noStart, collectionInterface, "Locked").Store(&property); err != nil {
		return provider.CredentialUnknown, "Cannot inspect the Secret Service lock state", nil
	}
	locked, ok := property.Value().(bool)
	if !ok {
		return provider.CredentialUnknown, "Secret Service returned invalid lock metadata", nil
	}
	if locked {
		return provider.CredentialLocked, "Secret Service collection is locked", nil
	}

	entries := make(map[provider.SessionKey]provider.CredentialEvidence, len(keys))
	for _, key := range keys {
		var items []dbus.ObjectPath
		attrs := map[string]string{"service": providerService, "username": credentialAccount(key)}
		if err := collection.CallWithContext(ctx, collectionInterface+".SearchItems", noStart, attrs).Store(&items); err != nil {
			continue
		}
		entries[key] = provider.CredentialAbsent
		if len(items) != 0 {
			entries[key] = provider.CredentialPresent
		}
	}
	return provider.CredentialAvailable, "Secret Service collection is unlocked", entries
}
