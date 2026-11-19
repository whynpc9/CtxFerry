//go:build !windows

package platform

import "context"

func currentUserSID() string { return "" }

func (s userSyncService) windowsState(context.Context) (bool, bool) { return false, false }

func (s userSyncService) installTask(context.Context, string, []string) (bool, error) {
	return false, nil
}
