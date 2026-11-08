package systemd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// UnknownName is the name Listeners gives a descriptor that LISTEN_FDNAMES does not name, as
// systemd itself does.
const UnknownName = "unknown"

// listenFDsStart is the first descriptor systemd passes, the one after standard error.
const listenFDsStart = 3

// maxListenFDs is the largest LISTEN_FDS Listeners accepts. It is far above any descriptor limit
// systemd runs a service with, and it bounds the descriptors a failed adoption closes, which
// would otherwise be as many as a corrupt LISTEN_FDS claims.
const maxListenFDs = 1 << 16

// Listeners adopts the listening sockets systemd passed to the process by socket activation,
// read from LISTEN_PID, LISTEN_FDS and LISTEN_FDNAMES through lookup, and returns them keyed by
// name.
//
// The descriptors are the LISTEN_FDS consecutive ones from 3, and the i-th name of the
// colon-separated LISTEN_FDNAMES (FileDescriptorName= in the .socket unit) is the name of the
// i-th descriptor. A descriptor with no name, because LISTEN_FDNAMES is absent, empty, holds
// fewer names than descriptors or an empty name in its place, is named UnknownName; names past the
// last descriptor are ignored. Descriptors sharing a name are collected under it in descriptor
// order. Each descriptor is wrapped in a net.Listener, which holds a duplicate of it, and the
// inherited descriptor is then closed, so closing the listener releases the socket in this
// process.
//
// The result is a nil map, with a nil error, when runtime.GOOS is not linux, when LISTEN_PID or
// LISTEN_FDS is absent or empty or LISTEN_FDS is 0, and when LISTEN_PID names a process other than
// this one, which means the variables were meant for another process and inherited by this one.
// LISTEN_PID is read first, so variables meant for another process are never an error.
//
// A LISTEN_PID that is not a positive integer, a LISTEN_FDS that is not a non-negative integer
// and a LISTEN_FDS above 65536 are errors, and no descriptor is touched. A descriptor that is not
// a listening stream socket, such as one from ListenDatagram=, is an error too: every listener
// opened so far is closed, and every inherited descriptor with it, so that none leaks into a
// process that cannot use it. Sockets of a unit with Accept=yes are connections, not listeners,
// and are not supported.
//
// Listeners is called once per process. A second call would wrap whatever has since reused the
// descriptor numbers the first one closed.
func Listeners(lookup func(key string) (string, bool)) (map[string][]net.Listener, error) {
	return listeners(runtime.GOOS, listenFDsStart, lookup)
}

// listeners is Listeners for the operating system goos, with the descriptors numbered from
// firstFD.
func listeners(goos string, firstFD int, lookup func(key string) (string, bool)) (map[string][]net.Listener, error) {
	if goos != "linux" {
		return nil, nil
	}
	rawPID, ok := lookup("LISTEN_PID")
	if !ok || rawPID == "" {
		return nil, nil
	}
	pid, err := parsePID("LISTEN_PID", rawPID)
	if err != nil {
		return nil, err
	}
	if pid != os.Getpid() {
		return nil, nil
	}
	rawFDs, ok := lookup("LISTEN_FDS")
	if !ok || rawFDs == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(rawFDs)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("systemd: LISTEN_FDS %q is not a non-negative integer", rawFDs)
	}
	if n > maxListenFDs {
		return nil, fmt.Errorf("systemd: LISTEN_FDS %q is too large", rawFDs)
	}
	if n == 0 {
		return nil, nil
	}
	rawNames, _ := lookup("LISTEN_FDNAMES")
	names := fdNames(rawNames, n)

	result := make(map[string][]net.Listener, n)
	opened := make([]net.Listener, 0, n)
	for i, name := range names {
		fd := firstFD + i
		l, err := adopt(fd)
		if err != nil {
			for _, o := range opened {
				_ = o.Close()
			}
			rest := fd + 1 // [lydite:exclude_from_mutation][fd-1 only re-closes descriptors already closed above]
			closeFDs(rest, firstFD+n)
			return nil, fmt.Errorf("systemd: listener %d (%s): %w", fd, name, err)
		}
		opened = append(opened, l)
		result[name] = append(result[name], l)
	}
	return result, nil
}

// fdNames returns the names of n descriptors from value, the colon-separated LISTEN_FDNAMES:
// the i-th name for the i-th descriptor, UnknownName where that name is missing or empty. Names
// past the n-th are dropped.
func fdNames(value string, n int) []string {
	var given []string
	if value != "" {
		given = strings.Split(value, ":")
	}
	names := make([]string, n)
	for i := range names {
		if i < len(given) && given[i] != "" {
			names[i] = given[i]
		} else {
			names[i] = UnknownName
		}
	}
	return names
}

// adopt wraps the inherited descriptor fd in a net.Listener and closes fd, whether or not the
// wrapping succeeds.
func adopt(fd int) (net.Listener, error) {
	f := os.NewFile(uintptr(fd), "LISTEN_FDS:"+strconv.Itoa(fd))
	if f == nil {
		return nil, errors.New("not a valid descriptor")
	}
	l, err := net.FileListener(f)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if l.Addr().Network() == "unixgram" {
		_ = l.Close()
		return nil, errors.New("not a stream socket")
	}
	if closeErr != nil {
		_ = l.Close()
		return nil, closeErr
	}
	return l, nil
}

// closeFDs closes the inherited descriptors from first up to, not including, end.
func closeFDs(first, end int) {
	for fd := first; fd < end; fd++ {
		if f := os.NewFile(uintptr(fd), ""); f != nil {
			_ = f.Close()
		}
	}
}
