package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// testRedactor redacts the baseline plus the prefix "ghp_" and the header "X-Api-Key".
func testRedactor() *redactor {
	return newRedactor(Redaction{Prefixes: []string{"ghp_"}, Headers: []string{"X-Api-Key"}})
}

// writeAll writes each chunk to w and fails the test on an error.
func writeAll(t *testing.T, w io.Writer, chunks ...string) {
	t.Helper()
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		if err != nil {
			t.Fatalf("Write(%q): %v", c, err)
		}
		if n != len(c) {
			t.Fatalf("Write(%q) = %d, want %d", c, n, len(c))
		}
	}
}

// filler returns n bytes that are neither token characters nor part of any prefix or header.
func filler(n int) string { return strings.Repeat("#", n) }

func TestRedactingWriterSplitAtEveryOffset(t *testing.T) {
	r := testRedactor()
	tests := []struct {
		name string
		mode redactMode
		in   string
	}{
		{"text", redactText, "x ghp_abc123 y\nAuthorization: Bearer zzz\nplain\nsent Basic dXNl= ok\n"},
		{"json", redactJSON, `{"msg":"Authorization: Bearer a\"b","k":"ghp_q1"}` + "\n" +
			`{"X-Api-Key":"v\\\"w","n":1}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want strings.Builder
			for line := range strings.Lines(tt.in) {
				want.WriteString(r.line(strings.TrimSuffix(line, "\n"), tt.mode) + "\n")
			}
			for i := range len(tt.in) + 1 {
				for j := i; j <= len(tt.in); j++ {
					var out bytes.Buffer
					w := newRedactingWriter(&out, r, tt.mode)
					writeAll(t, w, tt.in[:i], tt.in[i:j], tt.in[j:])
					if got := out.String(); got != want.String() {
						t.Fatalf("split at %d,%d: got %q, want %q", i, j, got, want.String())
					}
				}
			}
		})
	}
}

func TestRedactingWriterSplitAtPrefixBoundary(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"after tool prefix", []string{"token ghp_", "abc123 end\n"}, "token ghp_[REDACTED] end\n"},
		{"inside tool prefix", []string{"token ghp", "_abc123 end\n"}, "token ghp_[REDACTED] end\n"},
		{"after bearer prefix", []string{"Bearer ", "abc\n"}, "Bearer [REDACTED]\n"},
		{"after header separator", []string{"Cookie: ", "a=b\n"}, "Cookie: [REDACTED]\n"},
		{"inside header name", []string{"Set-Coo", "kie: sid=1\n"}, "Set-Cookie: [REDACTED]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor(), redactText)
			writeAll(t, w, tt.chunks...)
			if got := out.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactingWriterHoldsPartialLine(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor(), redactText)
	writeAll(t, w, "first\nsecond ghp_abc")
	if got := out.String(); got != "first\n" {
		t.Fatalf("before Flush: got %q, want %q", got, "first\n")
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := out.String(), "first\nsecond ghp_[REDACTED]"; got != want {
		t.Fatalf("after Flush: got %q, want %q", got, want)
	}
	// The token flushed at the end of the held bytes may continue; its remainder is redacted.
	writeAll(t, w, "def more\n")
	if got, want := out.String(), "first\nsecond ghp_[REDACTED] more\n"; got != want {
		t.Fatalf("after continuation: got %q, want %q", got, want)
	}
}

func TestRedactingWriterCloseEmitsRemainder(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor(), redactText)
	writeAll(t, w, "line\nAuthorization: secret")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := out.String(), "line\nAuthorization: [REDACTED]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// closeRecorder records whether Close was called.
type closeRecorder struct {
	bytes.Buffer
	closed bool
}

func (c *closeRecorder) Close() error { c.closed = true; return nil }

func TestRedactingWriterCloseLeavesDestinationOpen(t *testing.T) {
	dst := &closeRecorder{}
	w := newRedactingWriter(dst, testRedactor(), redactText)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dst.closed {
		t.Error("Close closed the destination")
	}
}

func TestRedactingWriterFlushEmptyWritesNothing(t *testing.T) {
	dst := &countingWriter{}
	w := newRedactingWriter(dst, testRedactor(), redactText)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if dst.calls != 0 {
		t.Errorf("Flush with nothing held made %d writes, want 0", dst.calls)
	}
}

// countingWriter counts its Write calls.
type countingWriter struct{ calls int }

func (c *countingWriter) Write(p []byte) (int, error) { c.calls++; return len(p), nil }

func TestRedactingWriterCapAndWindow(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor(), redactText)
	writeAll(t, w, filler(maxBuffered))
	if out.Len() != 0 {
		t.Fatalf("at the cap: %d bytes written, want 0", out.Len())
	}
	writeAll(t, w, "#")
	if got, want := out.Len(), maxBuffered+1-overflowWindow; got != want {
		t.Fatalf("past the cap: %d bytes written, want %d", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := out.String(), filler(maxBuffered+1); got != want {
		t.Errorf("after Flush: %d bytes, want %d", len(got), len(want))
	}
}

func TestRedactingWriterOverflowKeepsTokenInWindow(t *testing.T) {
	// The token starts inside the retained window, so it is redacted whole once the line ends.
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor(), redactText)
	head := filler(maxBuffered + 1 - overflowWindow + 10)
	writeAll(t, w, head+"ghp_SECRET", "MORE", filler(overflowWindow-20)+" end\n")
	want := head + "ghp_[REDACTED]" + filler(overflowWindow-20) + " end\n"
	if got := out.String(); got != want {
		t.Errorf("got suffix %q, want suffix %q", got[len(head)-5:], want[len(head)-5:])
	}
}

func TestRedactingWriterOverflowTokenStraddlingCut(t *testing.T) {
	r := testRedactor()
	cut := maxBuffered + 1 - overflowWindow
	tests := []struct {
		name   string
		mode   redactMode
		before string
		secret string
		after  string
		want   string
	}{
		{"prefixed token", redactText, "ghp_", "SECRET", " end\n", "#ghp_[REDACTED] end\n"},
		{"text header", redactText, "Authorization: ", "SECRET", "\n", "#Authorization: [REDACTED]\n"},
		{"json header value", redactJSON, `"msg":"Cookie: `, `SECRET\"Q`, `","n":1}` + "\n",
			`",` + `"msg":"Cookie: [REDACTED]","n":1}` + "\n"},
		{"json header key", redactJSON, `"X-Api-Key":"`, `SECRET`, `"}` + "\n",
			`",` + `"X-Api-Key":"[REDACTED]"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The secret starts before the cut and runs past the end of the buffer, so the
			// overflow flush emits a redacted head and the rest of the secret arrives later.
			head := filler(cut-10-len(tt.before)) + tt.before
			if tt.mode == redactJSON {
				// The filler sits inside a first string value, so the line is valid JSON.
				head = `{"pad":"` + filler(cut-10) + `",` + tt.before
			}
			body := strings.Repeat("S", overflowWindow+20)
			var out bytes.Buffer
			w := newRedactingWriter(&out, r, tt.mode)
			writeAll(t, w, head+body)
			if out.Len() == 0 {
				t.Fatal("overflow flushed nothing")
			}
			writeAll(t, w, tt.secret+"MORE", tt.after)
			got := out.String()
			if strings.Contains(got, "SECRET") || strings.Contains(got, "MORE") || strings.Contains(got, "SS") {
				t.Fatalf("secret leaked: %q", got[max(0, len(got)-200):])
			}
			if !strings.HasSuffix(got, tt.want) {
				t.Errorf("got suffix %q, want suffix %q", got[max(0, len(got)-len(tt.want)-20):], tt.want)
			}
			if tt.mode == redactJSON && !json.Valid(bytes.TrimSuffix(out.Bytes(), []byte("\n"))) {
				t.Errorf("output line is not valid JSON")
			}
		})
	}
}

func TestRedactingWriterOverflowJSONTrailingEscape(t *testing.T) {
	// The buffer ends on the backslash of an escape inside a header value, so the quote it
	// escapes arrives with the next write and does not end the value.
	r := testRedactor()
	pad := filler(maxBuffered - overflowWindow)
	var out bytes.Buffer
	w := newRedactingWriter(&out, r, redactJSON)
	writeAll(t, w, `{"pad":"`+pad+`","msg":"Authorization: `+strings.Repeat("S", overflowWindow)+`\`)
	writeAll(t, w, `"LEAK","n":1}`+"\n")
	got := out.String()
	if strings.Contains(got, "LEAK") || strings.Contains(got, "SS") {
		t.Fatalf("secret leaked: %q", got[max(0, len(got)-200):])
	}
	if want := `"msg":"Authorization: [REDACTED]","n":1}` + "\n"; !strings.HasSuffix(got, want) {
		t.Errorf("got suffix %q, want %q", got[max(0, len(got)-len(want)):], want)
	}
	if !json.Valid(bytes.TrimSuffix(out.Bytes(), []byte("\n"))) {
		t.Error("output line is not valid JSON")
	}
}

func TestRedactingWriterOverflowKeepsRunesWhole(t *testing.T) {
	// "é" is two bytes; an odd number of filler bytes in front puts the raw cut inside a rune.
	for _, lead := range []int{0, 1} {
		t.Run(fmt.Sprint(lead), func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor(), redactText)
			writeAll(t, w, filler(lead)+strings.Repeat("é", maxBuffered/2+1))
			if out.Len() == 0 {
				t.Fatal("overflow flushed nothing")
			}
			if !utf8.Valid(out.Bytes()) {
				t.Error("overflow flush split a rune")
			}
		})
	}
}

func TestRedactingWriterJSONLinesStayValid(t *testing.T) {
	lines := []string{
		`{"msg":"Authorization: Bearer abc","level":"INFO"}`,
		`{"msg":"x","Cookie":"a=b; c=\"d\"","n":2}`,
		`{"msg":"token ghp_abc and Basic dXNl= here","set-cookie":"sid=1"}`,
		`{"msg":"Proxy-Authorization: Basic \\\"q\\\" tail","x-api-key":"k\\"}`,
	}
	in := strings.Join(lines, "\n") + "\n"
	for _, size := range []int{1, 2, 3, 7, 64} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor(), redactJSON)
			for i := 0; i < len(in); i += size {
				writeAll(t, w, in[i:min(i+size, len(in))])
			}
			got := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if len(got) != len(lines) {
				t.Fatalf("%d lines, want %d: %q", len(got), len(lines), out.String())
			}
			for _, line := range got {
				if !json.Valid([]byte(line)) {
					t.Errorf("not valid JSON: %q", line)
				}
				for _, secret := range []string{"abc", "a=b", "sid=1", "dXNl", `\"q`, "k\\\\"} {
					if strings.Contains(line, secret) {
						t.Errorf("secret %q in %q", secret, line)
					}
				}
			}
		})
	}
}

func TestRedactingWriterConcurrentWriters(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor(), redactText)
	const writers, perWriter = 8, 200
	var wg sync.WaitGroup
	for g := range writers {
		wg.Go(func() {
			for i := range perWriter {
				line := fmt.Sprintf("writer %d line %d ghp_SECRET%d%d\n", g, i, g, i)
				if _, err := w.Write([]byte(line)); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(got) != writers*perWriter {
		t.Fatalf("%d lines, want %d", len(got), writers*perWriter)
	}
	for _, line := range got {
		if strings.Contains(line, "SECRET") || !strings.HasSuffix(line, " ghp_[REDACTED]") {
			t.Errorf("line not redacted whole: %q", line)
		}
	}
}

// failingWriter fails every write with err after writing n bytes.
type failingWriter struct {
	n   int
	err error
}

func (f *failingWriter) Write(p []byte) (int, error) { return min(f.n, len(p)), f.err }

func TestRedactingWriterDestinationError(t *testing.T) {
	errBoom := errors.New("boom")
	t.Run("write", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor(), redactText)
		if _, err := w.Write([]byte("line\n")); !errors.Is(err, errBoom) {
			t.Errorf("Write error = %v, want %v", err, errBoom)
		}
	})
	t.Run("partial line is not written", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor(), redactText)
		if n, err := w.Write([]byte("held")); err != nil || n != 4 {
			t.Errorf("Write = %d, %v; want 4, nil", n, err)
		}
	})
	t.Run("flush", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor(), redactText)
		writeAll(t, w, "held")
		if err := w.Flush(); !errors.Is(err, errBoom) {
			t.Errorf("Flush error = %v, want %v", err, errBoom)
		}
	})
	t.Run("close", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor(), redactText)
		writeAll(t, w, "held")
		if err := w.Close(); !errors.Is(err, errBoom) {
			t.Errorf("Close error = %v, want %v", err, errBoom)
		}
	})
	t.Run("short write", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{n: 2}, testRedactor(), redactText)
		if _, err := w.Write([]byte("line\n")); !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("Write error = %v, want %v", err, io.ErrShortWrite)
		}
	})
}
