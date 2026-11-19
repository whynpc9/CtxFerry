package app

// SetEnableHooks replaces the enable test seams and returns a restore func.
func SetEnableHooks(discovered, locked func()) func() {
	enableDiscovered, enableLocked = discovered, locked
	return func() { enableDiscovered, enableLocked = func() {}, func() {} }
}
