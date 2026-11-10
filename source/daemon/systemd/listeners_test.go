package systemd

import (
	"errors"
	"math"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// unopenedFD is a descriptor number above any limit Linux allows, so it is never open and a test
// that hands it to listeners never touches a descriptor another part of the process owns.
const unopenedFD = math.MaxInt32 - 1

func TestFDNames(t *testing.T) {
	tests := []struct {
		name  string
		value string
		n     int
		want  []string
	}{
		{"absent", "", 2, []string{UnknownName, UnknownName}},
		{"one per descriptor", "web:admin", 2, []string{"web", "admin"}},
		{"fewer names than descriptors", "web", 3, []string{"web", UnknownName, UnknownName}},
		{"more names than descriptors", "web:admin:metrics", 2, []string{"web", "admin"}},
		{"empty name", "web::metrics", 3, []string{"web", UnknownName, "metrics"}},
		{"duplicates kept in order", "web:web", 2, []string{"web", "web"}},
		{"no descriptors", "web", 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fdNames(tt.value, tt.n); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fdNames(%q, %d) = %q, want %q", tt.value, tt.n, got, tt.want)
			}
		})
	}
}

func TestListenersIsEmptyWhenNotActivated(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	other := strconv.Itoa(os.Getpid() + 1)
	for name, e := range map[string]env{
		"absent":                     {},
		"empty":                      {"LISTEN_PID": "", "LISTEN_FDS": ""},
		"no LISTEN_FDS":              {"LISTEN_PID": self},
		"empty LISTEN_FDS":           {"LISTEN_PID": self, "LISTEN_FDS": ""},
		"no LISTEN_PID":              {"LISTEN_FDS": "1"},
		"zero descriptors":           {"LISTEN_PID": self, "LISTEN_FDS": "0"},
		"other process":              {"LISTEN_PID": other, "LISTEN_FDS": "1"},
		"other process, malformed":   {"LISTEN_PID": other, "LISTEN_FDS": "many"},
		"other process, names given": {"LISTEN_PID": other, "LISTEN_FDS": "1", "LISTEN_FDNAMES": "web"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := listeners("linux", unopenedFD, inheritedFile, e.lookup)
			if err != nil || got != nil {
				t.Errorf("listeners = %v, %v; want nil, nil", got, err)
			}
		})
	}
}

func TestListenersIsEmptyOffLinux(t *testing.T) {
	got, err := listeners("darwin", unopenedFD, inheritedFile, untouched(t))
	if err != nil || got != nil {
		t.Errorf("listeners = %v, %v; want nil, nil", got, err)
	}
}

func TestListenersOutsideSystemdThroughTheExportedFunction(t *testing.T) {
	got, err := Listeners(env{}.lookup)
	if err != nil || got != nil {
		t.Errorf("Listeners = %v, %v; want nil, nil", got, err)
	}
}

func TestListenersRejectsMalformedValues(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	for name, e := range map[string]env{
		"pid not a number":  {"LISTEN_PID": "me", "LISTEN_FDS": "1"},
		"pid zero":          {"LISTEN_PID": "0", "LISTEN_FDS": "1"},
		"pid negative":      {"LISTEN_PID": "-1", "LISTEN_FDS": "1"},
		"fds not a number":  {"LISTEN_PID": self, "LISTEN_FDS": "many"},
		"fds negative":      {"LISTEN_PID": self, "LISTEN_FDS": "-1"},
		"fds past int":      {"LISTEN_PID": self, "LISTEN_FDS": "99999999999999999999"},
		"fds with a suffix": {"LISTEN_PID": self, "LISTEN_FDS": "1 "},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := listeners("linux", unopenedFD, inheritedFile, e.lookup)
			if err == nil || got != nil {
				t.Errorf("listeners = %v, %v; want nil and an error", got, err)
			}
		})
	}
}

func TestListenersRejectsACountAboveTheCap(t *testing.T) {
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": strconv.Itoa(maxListenFDs + 1)}
	got, err := listeners("linux", unopenedFD, inheritedFile, e.lookup)
	if err == nil || got != nil {
		t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want the count rejected as too large", err)
	}
}

// At the cap the count is accepted and the descriptors are adopted, from one that is never open,
// so the adoption fails on its first descriptor and closes the rest, none of them open either.
func TestListenersAcceptsACountAtTheCap(t *testing.T) {
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": strconv.Itoa(maxListenFDs)}
	got, err := listeners("linux", unopenedFD, inheritedFile, e.lookup)
	if err == nil || got != nil {
		t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
	}
	if strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want the count accepted", err)
	}
	if want := "listener " + strconv.Itoa(unopenedFD); !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want it to name %q", err, want)
	}
}

func TestListenersFailsOnADescriptorThatIsNotOpen(t *testing.T) {
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": "1", "LISTEN_FDNAMES": "web"}
	got, err := listeners("linux", unopenedFD, inheritedFile, e.lookup)
	if err == nil || got != nil {
		t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
	}
	if !strings.Contains(err.Error(), "web") {
		t.Errorf("err = %v, want it to name the descriptor web", err)
	}
}

// fakeFDBase is the first of the descriptor numbers the in-process tests hand to listeners. Each
// number is only a key of the files a test passes, so it never names a descriptor of the process.
const fakeFDBase = 3

// filesByFD returns a newFile for listeners that opens fakeFDBase+i as files[i], and nil for
// any other number. listeners closes what it opens through the *os.File, which owns the
// descriptor, so a second Close of one of them reports os.ErrClosed and never closes a
// descriptor reused since.
func filesByFD(files []*os.File) func(fd int) *os.File {
	return func(fd int) *os.File {
		if i := fd - fakeFDBase; i >= 0 && i < len(files) {
			return files[i]
		}
		return nil
	}
}

// skipWithoutSocketFiles skips the test where a socket cannot be opened as an *os.File.
func skipWithoutSocketFiles(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a socket has no *os.File on windows")
	}
}

// assertFileClosed fails the test unless f has already been closed.
func assertFileClosed(t *testing.T, i int, f *os.File) {
	t.Helper()
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("file %d: Close = %v, want os.ErrClosed", i, err)
	}
}

func TestListenersAdoptsSocketsInProcess(t *testing.T) {
	skipWithoutSocketFiles(t)
	var files []*os.File
	var addrs []string
	for range 4 {
		addr, f := listenTCP(t)
		files = append(files, f)
		addrs = append(addrs, addr)
	}
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": "4", "LISTEN_FDNAMES": "web:admin:web"}
	got, err := listeners("linux", fakeFDBase, filesByFD(files), e.lookup)
	if err != nil {
		t.Fatalf("listeners: %v", err)
	}
	t.Cleanup(func() {
		for _, ls := range got {
			for _, l := range ls {
				_ = l.Close()
			}
		}
	})
	want := map[string][]string{
		"web":       {addrs[0], addrs[2]},
		"admin":     {addrs[1]},
		UnknownName: {addrs[3]},
	}
	gotAddrs := make(map[string][]string, len(got))
	for name, ls := range got {
		for _, l := range ls {
			gotAddrs[name] = append(gotAddrs[name], l.Addr().String())
		}
	}
	if !reflect.DeepEqual(gotAddrs, want) {
		t.Fatalf("listeners = %v, want %v", gotAddrs, want)
	}
	for _, ls := range got {
		for _, l := range ls {
			assertAccepts(t, l)
		}
	}
	for i, f := range files {
		assertFileClosed(t, i, f)
	}
}

// failInProcess runs listeners over a TCP listener, bad and another TCP listener, with a fourth
// file past LISTEN_FDS, and checks that the adoption fails on bad, closes every file it was
// given, and leaves the fourth open.
func failInProcess(t *testing.T, bad *os.File) {
	t.Helper()
	_, first := listenTCP(t)
	_, last := listenTCP(t)
	past := pipe(t)
	files := []*os.File{first, bad, last, past}
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": "3", "LISTEN_FDNAMES": "web:datagram:admin"}
	got, err := listeners("linux", fakeFDBase, filesByFD(files), e.lookup)
	if err == nil || got != nil {
		t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
	}
	if !strings.Contains(err.Error(), "datagram") {
		t.Errorf("err = %v, want it to name the descriptor datagram", err)
	}
	for i, f := range files[:3] {
		assertFileClosed(t, i, f)
	}
	if _, err := past.Stat(); err != nil {
		t.Errorf("file past LISTEN_FDS: %v, want it open", err)
	}
}

func TestListenersFailsOnAUDPSocketInProcess(t *testing.T) {
	skipWithoutSocketFiles(t)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	f, err := udp.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	failInProcess(t, f)
}

func TestListenersFailsOnAUnixDatagramSocketInProcess(t *testing.T) {
	skipWithoutSocketFiles(t)
	gram := listenNotify(t, socketPath(t))
	f, err := gram.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	failInProcess(t, f)
}

func TestListenersFailsOnAnInvalidDescriptorAndClosesTheRest(t *testing.T) {
	skipWithoutSocketFiles(t)
	_, first := listenTCP(t)
	_, last := listenTCP(t)
	files := []*os.File{first, nil, last}
	e := env{"LISTEN_PID": strconv.Itoa(os.Getpid()), "LISTEN_FDS": "4", "LISTEN_FDNAMES": "web:missing"}
	got, err := listeners("linux", fakeFDBase, filesByFD(files), e.lookup)
	if err == nil || got != nil {
		t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
	}
	if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "not a valid descriptor") {
		t.Errorf("err = %v, want it to name the invalid descriptor missing", err)
	}
	assertFileClosed(t, 0, first)
	assertFileClosed(t, 2, last)
}

// The tests above hand listeners *os.File values; the ones below check the adoption of real
// inherited descriptors. Listeners closes the descriptors it adopts, so a test that handed it a
// descriptor number of its own process would leave the *os.File owning that number to close it a
// second time, by then perhaps a descriptor reused by something else. The descriptors are
// therefore passed to a child process at 3 and up, as systemd passes them, and the child,
// TestListenersChild, adopts them.

// childScenario is the variable that makes TestListenersChild run, naming what it checks.
const childScenario = "VIPENGELE_SYSTEMD_TEST_CHILD"

// childAddrs is the variable that hands the child the addresses of the descriptors it inherits.
const childAddrs = "VIPENGELE_SYSTEMD_TEST_ADDRS"

// The scenarios of TestListenersChild.
const (
	scenarioAdopt = "adopt"
	scenarioFail  = "fail"
)

// runChild runs TestListenersChild in a child process under scenario, with files as its
// descriptors from 3, and fails the test unless the child passes.
func runChild(t *testing.T, scenario string, files []*os.File, addrs []string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestListenersChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		childScenario+"="+scenario,
		childAddrs+"="+strings.Join(addrs, ","),
	)
	cmd.ExtraFiles = files
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestListenersChild") {
		t.Fatalf("child did not pass:\n%s", out)
	}
}

// listenTCP listens on a loopback port and returns the listener's address and a duplicate of
// its descriptor, both closed when the test ends.
func listenTCP(t *testing.T) (string, *os.File) {
	t.Helper()
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	f, err := l.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return l.Addr().String(), f
}

// pipe returns the read end of a pipe, closed with its write end when the test ends. A failure
// scenario passes it at descriptor 6, the first one past LISTEN_FDS, which Listeners leaves open.
func pipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r
}

func skipWithoutInheritedFiles(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a child process inherits no extra descriptors on windows")
	}
}

func TestListenersAdoptsInheritedSockets(t *testing.T) {
	skipWithoutInheritedFiles(t)
	var files []*os.File
	var addrs []string
	for range 4 {
		addr, f := listenTCP(t)
		files = append(files, f)
		addrs = append(addrs, addr)
	}
	runChild(t, scenarioAdopt, files, addrs)
}

func TestListenersFailsOnAUDPSocket(t *testing.T) {
	skipWithoutInheritedFiles(t)
	addr, tcp := listenTCP(t)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	udpFile, err := udp.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udpFile.Close() })
	_, last := listenTCP(t)

	runChild(t, scenarioFail, []*os.File{tcp, udpFile, last, pipe(t)}, []string{addr})
}

func TestListenersFailsOnAUnixDatagramSocket(t *testing.T) {
	skipWithoutInheritedFiles(t)
	addr, tcp := listenTCP(t)
	gram := listenNotify(t, socketPath(t))
	gramFile, err := gram.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gramFile.Close() })
	_, last := listenTCP(t)

	runChild(t, scenarioFail, []*os.File{tcp, gramFile, last, pipe(t)}, []string{addr})
}

// TestListenersChild is the child process of the tests above. It runs only when childScenario is
// set, and its descriptors from 3 are the ones its parent passed.
func TestListenersChild(t *testing.T) {
	scenario := os.Getenv(childScenario)
	if scenario == "" {
		t.Skip("runs only as the child process of TestListenersAdoptsInheritedSockets and the failure tests")
	}
	addrs := strings.Split(os.Getenv(childAddrs), ",")
	self := strconv.Itoa(os.Getpid())

	switch scenario {
	case scenarioAdopt:
		e := env{"LISTEN_PID": self, "LISTEN_FDS": "4", "LISTEN_FDNAMES": "web:admin:web"}
		got, err := listeners("linux", 3, inheritedFile, e.lookup)
		if err != nil {
			t.Fatalf("listeners: %v", err)
		}
		assertSockets(t, 4)
		t.Cleanup(func() {
			for _, ls := range got {
				for _, l := range ls {
					_ = l.Close()
				}
			}
		})
		want := map[string][]string{
			"web":       {addrs[0], addrs[2]},
			"admin":     {addrs[1]},
			UnknownName: {addrs[3]},
		}
		gotAddrs := make(map[string][]string, len(got))
		for name, ls := range got {
			for _, l := range ls {
				gotAddrs[name] = append(gotAddrs[name], l.Addr().String())
			}
		}
		if !reflect.DeepEqual(gotAddrs, want) {
			t.Fatalf("listeners = %v, want %v", gotAddrs, want)
		}
		assertAccepts(t, got["web"][0])

	case scenarioFail:
		e := env{"LISTEN_PID": self, "LISTEN_FDS": "3", "LISTEN_FDNAMES": "web:datagram:admin"}
		got, err := listeners("linux", 3, inheritedFile, e.lookup)
		if err == nil || got != nil {
			t.Fatalf("listeners = %v, %v; want nil and an error", got, err)
		}
		if !strings.Contains(err.Error(), "datagram") {
			t.Errorf("err = %v, want it to name the descriptor datagram", err)
		}
		assertSockets(t, 0)
		assertClosed(t, 3, 6)
		assertOpen(t, 6)

	default:
		t.Fatalf("unknown scenario %q", scenario)
	}
}

// assertClosed fails the test unless every descriptor from first up to, not including, end is
// closed. It runs while the process holds no descriptor that could have taken those numbers, so
// closing the probe never closes a descriptor in use.
func assertClosed(t *testing.T, first, end int) {
	t.Helper()
	for fd := first; fd < end; fd++ {
		f := os.NewFile(uintptr(fd), "")
		_, err := f.Stat()
		_ = f.Close()
		if err == nil {
			t.Errorf("descriptor %d is open", fd)
		}
	}
}

// assertOpen fails the test unless descriptor fd is open. It checks on linux only, where
// /proc/self/fd lists the descriptors, and never closes fd; elsewhere it does nothing.
func assertOpen(t *testing.T, fd int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	if _, err := os.Lstat("/proc/self/fd/" + strconv.Itoa(fd)); err != nil {
		t.Errorf("descriptor %d is not open: %v", fd, err)
	}
}

// assertSockets fails the test unless the process holds exactly want sockets. It checks on linux
// only, where /proc/self/fd lists the descriptors; elsewhere it does nothing.
func assertSockets(t *testing.T, want int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, e := range entries {
		target, err := os.Readlink("/proc/self/fd/" + e.Name())
		if err == nil && strings.HasPrefix(target, "socket:") {
			got++
		}
	}
	if got != want {
		t.Errorf("process holds %d sockets, want %d", got, want)
	}
}

// assertAccepts fails the test unless l accepts a connection.
func assertAccepts(t *testing.T, l net.Listener) {
	t.Helper()
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	c, err := net.Dial(l.Addr().Network(), l.Addr().String())
	if err != nil {
		t.Fatalf("dial %s: %v", l.Addr(), err)
	}
	_ = c.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("accept: %v", err)
	}
}
