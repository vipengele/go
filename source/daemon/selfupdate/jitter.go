package selfupdate

import (
	"hash/fnv"
	"time"
)

// jitter returns the delay of the first update check of the binary at path: the FNV-1a hash of
// path modulo a tenth of interval. It is the same for every start of the same binary and always
// below interval/10, so daemons installed at different paths spread their first checks over that
// window rather than all asking the release server at once. An interval below 10ns has no window
// and gives 0.
func jitter(path string, interval time.Duration) time.Duration {
	window := interval / 10
	if window <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	return time.Duration(h.Sum64() % uint64(window))
}
