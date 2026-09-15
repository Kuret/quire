package download

import "sync"

// ResetMemoryLimitOnce lets a test observe New applying the soft memory limit,
// which otherwise happens once per process and is already spent by the time
// most tests run.
func ResetMemoryLimitOnce() { memLimitOnce = sync.Once{} }
