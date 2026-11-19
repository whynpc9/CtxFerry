// bw is a synthetic command fixture. It never accesses a vault.
package main

import (
	"fmt"
	"os"
	"slices"
)

const session = "ctxferry-synthetic-provider-session"
const password = "ctxferry-synthetic-entry-value"
const item = "32f7cf3b-cb32-4944-8eeb-17d088b5a588"

func main() {
	for _, key := range []string{"BW_PASSWORD", "BW_CLIENTSECRET", "BW_CLEANEXIT", "CTXFERRY_BITWARDEN_DEVICE_ID", "GH_TOKEN"} {
		if _, exists := os.LookupEnv(key); exists {
			os.Exit(97)
		}
	}
	if info, err := os.Stat(os.Getenv("BITWARDENCLI_APPDATA_DIR")); err != nil || !info.IsDir() {
		os.Exit(98)
	}
	args := os.Args[1:]
	if slices.Equal(args, []string{"status", "--nointeraction"}) {
		switch os.Getenv("BW_SESSION") {
		case session:
			fmt.Print(`{"status":"unlocked","userEmail":"fixture@example.invalid"}`)
		default:
			fmt.Print(`{"status":"locked","userEmail":"fixture@example.invalid"}`)
		}
		return
	}
	if os.Getenv("BW_SESSION") != session || len(args) != 5 ||
		!slices.Equal(args[:2], []string{"get", "password"}) ||
		!slices.Equal(args[3:], []string{"--response", "--nointeraction"}) {
		os.Exit(99)
	}

	// Diagnostics from bw must stay private on both success and failure.
	fmt.Fprint(os.Stderr, session, password)
	if args[2] != item {
		fmt.Printf(`{"success":false,"message":"Not found. %s"}`, password)
		os.Exit(1)
	}
	fmt.Printf(`{"success":true,"data":{"object":"string","data":%q}}`, password)
}
