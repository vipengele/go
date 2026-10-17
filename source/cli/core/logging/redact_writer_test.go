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
		in   string
	}{
		{"text", "x ghp_abc123 y\nAuthorization: Bearer zzz\nplain\nsent Basic dXNl= ok\n"},
		{"scheme case", "auth bearer abc\nsent BASIC xyz= ok\n{\"auth\":\"bEaReR tok\"}\nGHP_abc\n"},
		{"json", `{"msg":"Authorization: Bearer a\"b","k":"ghp_q1"}` + "\n" +
			`{"X-Api-Key":"v\\\"w","n":1}` + "\n"},
		{"mixed shapes", `Set-Cookie: sid="abc123"; Path=/` + "\n" + `{"Cookie":"sid=abc"}` + "\n" +
			`  {"x-api-key" : "k1", "n":1}` + "\n" + `{"Cookie":"cut` + "\n" + `plain "X-Api-Key":"k2"` + "\n"},
		{"whole names", "X-Cookie: v\nsent Cookie: w\n" + `{"X-Authorization":"v","msg":"a\nCookie: w","n":1}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want strings.Builder
			for line := range strings.Lines(tt.in) {
				want.WriteString(r.line(strings.TrimSuffix(line, "\n")) + "\n")
			}
			for i := range len(tt.in) + 1 {
				for j := i; j <= len(tt.in); j++ {
					var out bytes.Buffer
					w := newRedactingWriter(&out, r)
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
		{"inside lower-case scheme", []string{"bea", "rer abc\n"}, "bearer [REDACTED]\n"},
		{"after header separator", []string{"Cookie: ", "a=b\n"}, "Cookie: [REDACTED]\n"},
		{"inside header name", []string{"Set-Coo", "kie: sid=1\n"}, "Set-Cookie: [REDACTED]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor())
			writeAll(t, w, tt.chunks...)
			if got := out.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactingWriterHoldsPartialLine(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
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

func TestRedactingWriterFlushInsideMixedCaseSchemeToken(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
	writeAll(t, w, "x bEaReR tok")
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	writeAll(t, w, "en more\n")
	if got, want := out.String(), "x bEaReR [REDACTED] more\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRedactingWriterFlushInsideLongerName pins that a header name at the start of a piece is
// recognised whatever ends the piece before it, while a piece that starts with the "-" of a longer
// name holds no header name.
func TestRedactingWriterFlushInsideLongerName(t *testing.T) {
	for _, tt := range []struct{ head, rest, want string }{
		{"X-", "Cookie: v\n", "X-Cookie: [REDACTED]\n"},
		{"X", "-Cookie: v\n", "X-Cookie: v\n"},
	} {
		var out bytes.Buffer
		w := newRedactingWriter(&out, testRedactor())
		writeAll(t, w, tt.head)
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
		writeAll(t, w, tt.rest)
		if got := out.String(); got != tt.want {
			t.Errorf("%q then %q: got %q, want %q", tt.head, tt.rest, got, tt.want)
		}
	}
}

func TestRedactingWriterCloseEmitsRemainder(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
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
	w := newRedactingWriter(dst, testRedactor())
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dst.closed {
		t.Error("Close closed the destination")
	}
}

func TestRedactingWriterFlushEmptyWritesNothing(t *testing.T) {
	dst := &countingWriter{}
	w := newRedactingWriter(dst, testRedactor())
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
	w := newRedactingWriter(&out, testRedactor())
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
	w := newRedactingWriter(&out, testRedactor())
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
		json   bool
		before string
		secret string
		after  string
		want   string
	}{
		{"prefixed token", false, "ghp_", "SECRET", " end\n", "#ghp_[REDACTED] end\n"},
		{"mixed-case scheme", false, "bEaReR ", "SECRET", " end\n", "#bEaReR [REDACTED] end\n"},
		{"text header", false, "Authorization: ", "SECRET", "\n", "#Authorization: [REDACTED]\n"},
		{"json header value", true, `"msg":"Cookie: `, `SECRET\"Q`, `","n":1}` + "\n",
			`",` + `"msg":"Cookie: [REDACTED]","n":1}` + "\n"},
		{"json header key", true, `"X-Api-Key":"`, `SECRET`, `"}` + "\n",
			`",` + `"X-Api-Key":"[REDACTED]"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The secret starts before the cut and runs past the end of the buffer, so the
			// overflow flush emits a redacted head and the rest of the secret arrives later.
			head := filler(cut-10-len(tt.before)) + tt.before
			if tt.json {
				// The filler sits inside a first string value, so the line is valid JSON.
				head = `{"pad":"` + filler(cut-10) + `",` + tt.before
			}
			body := strings.Repeat("S", overflowWindow+20)
			var out bytes.Buffer
			w := newRedactingWriter(&out, r)
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
			if tt.json && !json.Valid(bytes.TrimSuffix(out.Bytes(), []byte("\n"))) {
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
	line := `{"pad":"` + pad + `","msg":"Authorization: ` + strings.Repeat("S", overflowWindow) + `\`
	for _, earlier := range []string{"", filler(len(line)) + "\n"} {
		t.Run(fmt.Sprint(len(earlier) > 0), func(t *testing.T) {
			// An earlier line of the same length in the same write leaves its newline in the
			// buffer's memory just past the held bytes, where a piece that ran one byte past
			// them would pick it up.
			var out bytes.Buffer
			w := newRedactingWriter(&out, r)
			writeAll(t, w, earlier+line)
			writeAll(t, w, `"LEAK","n":1}`+"\n")
			got := strings.TrimPrefix(out.String(), earlier)
			if strings.Contains(got, "LEAK") || strings.Contains(got, "SS") {
				t.Fatalf("secret leaked: %q", got[max(0, len(got)-200):])
			}
			if want := `"msg":"Authorization: [REDACTED]","n":1}` + "\n"; !strings.HasSuffix(got, want) {
				t.Errorf("got suffix %q, want %q", got[max(0, len(got)-len(want)):], want)
			}
			if !json.Valid([]byte(strings.TrimSuffix(got, "\n"))) {
				t.Error("output line is not valid JSON")
			}
		})
	}
}

func TestRedactingWriterOverflowKeepsRunesWhole(t *testing.T) {
	cut := maxBuffered + 1 - overflowWindow
	for _, r := range []string{"é", "€", "😀"} {
		for back := 1; back < len(r); back++ {
			t.Run(fmt.Sprintf("%s/%d", r, back), func(t *testing.T) {
				// The raw cut falls on the back-th byte of the rune, so the window starts back
				// bytes earlier, at the rune's first byte.
				in := filler(cut-back) + r + filler(maxBuffered+1-(cut-back)-len(r))
				var out bytes.Buffer
				w := newRedactingWriter(&out, testRedactor())
				writeAll(t, w, in)
				if got, want := out.Len(), cut-back; got != want {
					t.Fatalf("overflow flushed %d bytes, want %d", got, want)
				}
				if err := w.Flush(); err != nil {
					t.Fatalf("Flush: %v", err)
				}
				if out.String() != in {
					t.Error("output differs from the input")
				}
			})
		}
	}
}

func TestRedactingWriterOverflowBacksUpAtMostARune(t *testing.T) {
	// Six continuation bytes end at the raw cut, which no valid UTF-8 holds: the window starts
	// utf8.UTFMax-1 bytes before the cut, inside the run.
	cut := maxBuffered + 1 - overflowWindow
	run := strings.Repeat("\x80", 6)
	in := filler(cut-5) + run + filler(maxBuffered+1-(cut-5)-len(run))
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
	writeAll(t, w, in)
	if got, want := out.Len(), cut-(utf8.UTFMax-1); got != want {
		t.Fatalf("overflow flushed %d bytes, want %d", got, want)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out.String() != in {
		t.Error("output differs from the input")
	}
}

// TestRedactingWriterOverflowSecretAtTheCut pins that a secret that starts exactly at the start
// of the retained window is left whole in the window: the overflow flush writes nothing past it,
// and the line is redacted whole when it ends.
func TestRedactingWriterOverflowSecretAtTheCut(t *testing.T) {
	r := testRedactor()
	cut := maxBuffered + 1 - overflowWindow
	tests := []struct {
		name, head, secret, end string
	}{
		{"prefixed token", filler(cut), "ghp_SECRET ", "\n"},
		{"text header", filler(cut), "Authorization: SECRET ", "\n"},
		{"json header key", `{"pad":"` + filler(cut-10) + `",`, `"Cookie":[1],"n":"`, `"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.head) != cut {
				t.Fatalf("head is %d bytes, want %d", len(tt.head), cut)
			}
			in := tt.head + tt.secret + filler(maxBuffered+1-cut-len(tt.secret))
			var out bytes.Buffer
			w := newRedactingWriter(&out, r)
			writeAll(t, w, in)
			if got := out.Len(); got != cut {
				t.Fatalf("overflow flushed %d bytes, want %d", got, cut)
			}
			writeAll(t, w, tt.end)
			whole := in + strings.TrimSuffix(tt.end, "\n")
			if got, want := out.String(), r.line(whole)+"\n"; got != want {
				t.Errorf("got suffix %q, want suffix %q", got[max(0, len(got)-300):], want[max(0, len(want)-300):])
			}
		})
	}
}

// TestRedactingWriterContinuationDropsEveryTokenChar pins that the rest of a flushed token is
// dropped up to the first byte that is not a token character, including the bytes at each end of
// every range of them.
func TestRedactingWriterContinuationDropsEveryTokenChar(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
	writeAll(t, w, "x ghp_q")
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	writeAll(t, w, "AZaz09+/=.~-_:@[`{ end\n")
	if got, want := out.String(), "x ghp_[REDACTED]:@[`{ end\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
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
			w := newRedactingWriter(&out, testRedactor())
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
	w := newRedactingWriter(&out, testRedactor())
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
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor())
		if _, err := w.Write([]byte("line\n")); !errors.Is(err, errBoom) {
			t.Errorf("Write error = %v, want %v", err, errBoom)
		}
	})
	t.Run("partial line is not written", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor())
		if n, err := w.Write([]byte("held")); err != nil || n != 4 {
			t.Errorf("Write = %d, %v; want 4, nil", n, err)
		}
	})
	t.Run("flush", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor())
		writeAll(t, w, "held")
		if err := w.Flush(); !errors.Is(err, errBoom) {
			t.Errorf("Flush error = %v, want %v", err, errBoom)
		}
	})
	t.Run("close", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{err: errBoom}, testRedactor())
		writeAll(t, w, "held")
		if err := w.Close(); !errors.Is(err, errBoom) {
			t.Errorf("Close error = %v, want %v", err, errBoom)
		}
	})
	t.Run("short write", func(t *testing.T) {
		w := newRedactingWriter(&failingWriter{n: 2}, testRedactor())
		if _, err := w.Write([]byte("line\n")); !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("Write error = %v, want %v", err, io.ErrShortWrite)
		}
	})
}

// TestRedactingWriterPiecedLineKeepsItsShape pins that a line flushed before its end is read in
// every piece by the shape of its start: a JSON object stays valid JSON, and a plain line drops a
// quoted value to its end.
func TestRedactingWriterPiecedLineKeepsItsShape(t *testing.T) {
	tests := []struct {
		name, head, rest, want string
	}{
		{"json object", ` {"Cookie":"ab`, `c\"d","n":1}` + "\n", ` {"Cookie":"[REDACTED]","n":1}` + "\n"},
		{"plain line", `Set-Cookie: sid="ab`, `c"; Path=/` + "\n", "Set-Cookie: [REDACTED]\n"},
		{"json array is text", `[{"Cookie":"ab`, `c"}]` + "\n", `[{"Cookie":[REDACTED]` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor())
			writeAll(t, w, tt.head)
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}
			writeAll(t, w, tt.rest, "next Cookie: x\n")
			if want := tt.want + "next Cookie: [REDACTED]\n"; out.String() != want {
				t.Errorf("got %q, want %q", out.String(), want)
			}
		})
	}
}

// TestRedactingWriterFlushBraceLineThatIsNotJSON pins that a held line starting with "{" that is
// not JSON is flushed as text, so a header value in it runs to the end of the line instead of
// stopping at its first quote.
func TestRedactingWriterFlushBraceLineThatIsNotJSON(t *testing.T) {
	tests := []struct {
		name, in, want string
		secrets        []string
	}{
		{"cookie", `{Cookie: sid="abc"; Path=/`, "{Cookie: [REDACTED]", []string{"abc", "Path"}},
		{"go value dump", `{Header:map[Authorization:[Token "s3cr3t"]]}`,
			"{Header:map[Authorization:[REDACTED]", []string{"s3cr3t", "Token"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor())
			writeAll(t, w, tt.in)
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}
			got := out.String()
			for _, s := range tt.secrets {
				if strings.Contains(got, s) {
					t.Errorf("%q leaked: %q", s, got)
				}
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactingWriterFlushWholeJSONLineStaysValid(t *testing.T) {
	for _, in := range []string{
		`{"msg":"Authorization: Bearer abc","Cookie":"a=\"b\"","n":1}`,
		`  [{"Cookie":"a=b"},"Proxy-Authorization: Basic dXNl"]`,
	} {
		t.Run(in, func(t *testing.T) {
			var out bytes.Buffer
			w := newRedactingWriter(&out, testRedactor())
			writeAll(t, w, in)
			if err := w.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}
			got := out.String()
			if !json.Valid([]byte(got)) {
				t.Errorf("not valid JSON: %q", got)
			}
			for _, s := range []string{"abc", "a=", "dXNl"} {
				if strings.Contains(got, s) {
					t.Errorf("%q leaked: %q", s, got)
				}
			}
		})
	}
}

func TestRedactingWriterOverflowBraceLineThatIsNotJSON(t *testing.T) {
	// The line starts with "{" but is not JSON, and its header value straddles the overflow cut.
	head := "{Header:map[" + filler(maxBuffered-overflowWindow) + " Authorization:[Token \"s3cr3t"
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
	writeAll(t, w, head+strings.Repeat("S", overflowWindow))
	if out.Len() == 0 {
		t.Fatal("overflow flushed nothing")
	}
	writeAll(t, w, `MORE"]]}`+"\n")
	got := out.String()
	if strings.Contains(got, "s3cr3t") || strings.Contains(got, "MORE") || strings.Contains(got, "SS") {
		t.Fatalf("secret leaked: %q", got[max(0, len(got)-200):])
	}
	if want := " Authorization:[REDACTED]\n"; !strings.HasSuffix(got, want) {
		t.Errorf("got suffix %q, want %q", got[max(0, len(got)-len(want)-20):], want)
	}
}

func TestShapeOfStart(t *testing.T) {
	tests := []struct {
		in   string
		want lineShape
	}{
		{`{"a":1}`, shapeJSON},
		{`[1,"x"]`, shapeJSON},
		{` {"Cookie":"ab`, shapeJSON},
		{`{"a":"x\`, shapeJSON},
		{`{"a":tr`, shapeJSON},
		{`{"a":12`, shapeJSON},
		{`{ "a" : { "b" :`, shapeJSON},
		{`{`, shapeJSON},
		{`{Cookie: sid="abc"`, shapeText},
		{`{Header:map[Authorization:[Token "s3cr3t"]]}`, shapeText},
		{`{"a":1} Cookie: "x"`, shapeText},
		{`{"a" 1`, shapeText},
		{`[{"Cookie":"ab`, shapeText},
		{`[INFO] Cookie: "x"`, shapeText},
		{"", shapeText},
		{" \t", shapeText},
	}
	for _, tt := range tests {
		if got := shapeOfStart([]byte(tt.in)); got != tt.want {
			t.Errorf("shapeOfStart(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRedactorJSONHeaderValueOfAnyType(t *testing.T) {
	r := testRedactor()
	tests := []struct {
		name, in, want string
	}{
		{"array", `{"Cookie":["sid=abc"]}`, `{"Cookie":"[REDACTED]"}`},
		{"object", `{"Cookie":{"a":"b"}}`, `{"Cookie":"[REDACTED]"}`},
		{"number", `{"Cookie":123,"n":1}`, `{"Cookie":"[REDACTED]","n":1}`},
		{"negative number", `{"x-api-key":-1.5e3}`, `{"x-api-key":"[REDACTED]"}`},
		{"bool", `{"Cookie":true}`, `{"Cookie":"[REDACTED]"}`},
		{"null", `{"Cookie":null}`, `{"Cookie":"[REDACTED]"}`},
		{"spacing kept", `{ "Cookie" : [ "a" , "b" ] , "n" : 1 }`, `{ "Cookie" : "[REDACTED]" , "n" : 1 }`},
		{"nested", `{"req":{"In":{"Header":{"Cookie":["sid=abc"],"Accept":["*/*"]}}}}`,
			`{"req":{"In":{"Header":{"Cookie":"[REDACTED]","Accept":["*/*"]}}}}`},
		{"in an array", `[{"Set-Cookie":["a=1","b=2"]},{"Cookie":{"x":[1,{"y":"]"}]}}]`,
			`[{"Set-Cookie":"[REDACTED]"},{"Cookie":"[REDACTED]"}]`},
		{"brackets and quotes in strings", `{"Cookie":["a]\"}","{"],"n":1}`, `{"Cookie":"[REDACTED]","n":1}`},
		{"repeated key", `{"Cookie":1,"cookie":[2],"COOKIE":"3"}`,
			`{"Cookie":"[REDACTED]","cookie":"[REDACTED]","COOKIE":"[REDACTED]"}`},
		{"escaped key text in a string", `{"msg":"see \"Cookie\": [1]"}`, `{"msg":"see \"Cookie\": [1]"}`},
		{"key after an escaped backslash", `{"a":"x\\","Cookie":[1]}`, `{"a":"x\\","Cookie":"[REDACTED]"}`},
		{"other key ending in a name", `{"X-Cookie":[1]}`, `{"X-Cookie":[1]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.line(tt.in)
			if got != tt.want {
				t.Errorf("line(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("line(%q) = %q, not valid JSON", tt.in, got)
			}
			if again := r.line(got); again != got {
				t.Errorf("line(%q) = %q, want it unchanged", got, again)
			}
		})
	}
}

func TestRedactingWriterJSONHeaderValueSplitAtEveryOffset(t *testing.T) {
	r := testRedactor()
	in := `{"Cookie":["sid=abc","x\"]"],"n":1}` + "\n" +
		`{"h":{"Cookie":{"a":{"b":"c"}},"X-Api-Key":12345}}` + "\n" +
		`{"Set-Cookie":true,"Cookie":null}` + "\n"
	var want strings.Builder
	for line := range strings.Lines(in) {
		want.WriteString(r.line(strings.TrimSuffix(line, "\n")) + "\n")
	}
	for i := range len(in) + 1 {
		for j := i; j <= len(in); j++ {
			var out bytes.Buffer
			w := newRedactingWriter(&out, r)
			writeAll(t, w, in[:i], in[i:j], in[j:])
			if got := out.String(); got != want.String() {
				t.Fatalf("split at %d,%d: got %q, want %q", i, j, got, want.String())
			}
		}
	}
}

func TestRedactingWriterOverflowJSONHeaderValueStraddlingCut(t *testing.T) {
	r := testRedactor()
	cut := maxBuffered + 1 - overflowWindow
	tests := []struct {
		name, open, more, after, want string
	}{
		{"array", `["SECRET","S`, `MORE\"]"]`, `,"n":1}`, `"Cookie":"[REDACTED]","n":1}`},
		{"object", `{"a":{"b":"SECRET\\`, `MORE"}}`, `,"n":1}`, `"Cookie":"[REDACTED]","n":1}`},
		{"number", `1234`, `5678`, `,"n":1}`, `"Cookie":"[REDACTED]","n":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The value starts before the cut and runs past the end of the buffer, so the overflow
			// flush emits the redacted value and the rest of it is dropped as it arrives.
			head := `{"pad":"` + filler(cut-20) + `","Cookie":` + tt.open
			body := strings.Repeat("S", overflowWindow+20)
			if tt.name == "number" {
				body = strings.Repeat("9", overflowWindow+20)
			}
			var out bytes.Buffer
			w := newRedactingWriter(&out, r)
			writeAll(t, w, head+body)
			if out.Len() == 0 {
				t.Fatal("overflow flushed nothing")
			}
			writeAll(t, w, tt.more, tt.after+"\n")
			got := out.String()
			for _, s := range []string{"SECRET", "MORE", "SS", "99", "1234", "5678"} {
				if strings.Contains(got, s) {
					t.Fatalf("%q leaked: %q", s, got[max(0, len(got)-200):])
				}
			}
			if !strings.HasSuffix(got, tt.want+"\n") {
				t.Errorf("got suffix %q, want suffix %q", got[max(0, len(got)-len(tt.want)-20):], tt.want)
			}
			if !json.Valid(bytes.TrimSuffix(out.Bytes(), []byte("\n"))) {
				t.Error("output line is not valid JSON")
			}
		})
	}
}

func TestRedactingWriterFlushInsideJSONHeaderValue(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out, testRedactor())
	writeAll(t, w, `{"Cookie":["sid=a`)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	writeAll(t, w, `bc","[\"]"],"n":1}`+"\n", `{"Cookie":["x"]}`+"\n")
	want := `{"Cookie":"[REDACTED]","n":1}` + "\n" + `{"Cookie":"[REDACTED]"}` + "\n"
	if got := out.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
