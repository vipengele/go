package logging

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// maxBuffered is the most bytes a [RedactingWriter] holds without a newline. Past it, an overflow
// flush writes all but the last [overflowWindow] bytes.
const maxBuffered = 64 << 10

// overflowWindow is the tail an overflow flush retains, so that a prefix or header name that
// starts near the flush point is still followed by enough of its secret to be recognised when
// the rest of the line arrives.
const overflowWindow = 256

// continuation is what of a secret a [RedactingWriter] still drops from the bytes it receives,
// after a flush that ended inside the secret.
type continuation int

const (
	// continueNone drops nothing.
	continueNone continuation = iota
	// continueToken drops the token characters that follow a flushed prefixed token.
	continueToken
	// continueText drops a text header value up to the end of the line.
	continueText
	// continueJSON drops a JSON header value up to its closing unescaped quote or the end of the
	// line.
	continueJSON
	// continueJSONEscape drops the byte a flushed backslash escapes, then continues as
	// continueJSON.
	continueJSONEscape
)

// RedactingWriter is an [io.Writer] that redacts every secret of a [Redaction], and of the
// baseline, from the lines written to it before they reach its destination. It reads its input
// in the format of the handler that writes to it: in text, a header value runs to the end of the
// line; in JSON, it ends at the closing unescaped quote, so every redacted line stays valid JSON.
//
// It is line-buffered: a complete line is redacted whole and written with the Write that
// completes it, and the bytes after the last newline are held. A line longer than 64 KiB is
// written in pieces: once more than that is held, the writer redacts and writes everything but
// the last 256 bytes, never splitting a UTF-8 sequence, and extends the piece over any secret
// that crosses its end. When a secret runs to the end of what is held, the piece ends with the
// redacted secret and the rest of it is dropped as it arrives. A piece is therefore not a line
// on its own, and in JSON mode not a JSON document, but the destination's lines are the
// redacted lines: the pieces of one line concatenate to that line redacted whole.
//
// A RedactingWriter is safe for concurrent use; each Write is redacted and written whole before
// the next starts.
type RedactingWriter struct {
	mu   sync.Mutex
	dst  io.Writer
	r    *redactor
	mode redactMode
	// buf holds the bytes after the last newline written.
	buf []byte
	// cont is what of a flushed secret is still dropped. It is continueNone whenever buf holds
	// anything.
	cont continuation
}

// newRedactingWriter returns a writer that redacts with r, reading lines as mode, and writes to
// dst.
func newRedactingWriter(dst io.Writer, r *redactor, mode redactMode) *RedactingWriter {
	return &RedactingWriter{dst: dst, r: r, mode: mode}
}

// Write redacts and writes every line p completes and holds the rest. It returns len(p), the
// bytes it took from p, and the destination's error, or [io.ErrShortWrite] for a short write
// with none. The output of a failed write is dropped, not retried.
func (w *RedactingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, w.skipContinuation(p)...)
	var out []byte
	if i := bytes.LastIndexByte(w.buf, '\n'); i >= 0 {
		for line := range bytes.Lines(w.buf[:i+1]) {
			out = append(out, w.r.line(string(line[:len(line)-1]), w.mode)...)
			out = append(out, '\n')
		}
		w.buf = w.buf[:copy(w.buf, w.buf[i+1:])]
	}
	if len(w.buf) > maxBuffered {
		out = w.appendPiece(out, w.windowStart())
	}
	return len(p), w.emit(out)
}

// Flush redacts and writes the bytes held after the last newline, as a piece of their line.
// What it writes is redacted as it stands: a prefix or header name the held bytes end inside is
// not recognised once the rest of it arrives.
func (w *RedactingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	return w.emit(w.appendPiece(nil, len(w.buf)))
}

// Close flushes the held bytes, as [RedactingWriter.Flush] does. It does not close the
// destination, which stays the caller's to close.
func (w *RedactingWriter) Close() error { return w.Flush() }

// windowStart returns where the window an overflow flush retains begins, moved back to the start
// of the UTF-8 sequence it would split.
func (w *RedactingWriter) windowStart() int {
	cut := len(w.buf) - overflowWindow
	for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(w.buf[cut]); i++ {
		cut--
	}
	return cut
}

// appendPiece appends to out the redacted bytes of buf up to cut, extended over every secret
// that crosses it, removes them from buf and records what of a secret that runs to the end of
// buf is still to be dropped.
func (w *RedactingWriter) appendPiece(out []byte, cut int) []byte {
	end, cont := w.pieceEnd(cut)
	head := w.buf[:end]
	if cont == continueJSONEscape {
		// The trailing backslash escapes a byte not yet written; written alone it would
		// escape the next byte of the output instead.
		head = head[:len(head)-1]
	}
	out = append(out, w.r.line(string(head), w.mode)...)
	w.buf = w.buf[:copy(w.buf, w.buf[end:])]
	w.cont = cont
	return out
}

// pieceEnd returns cut extended to the end of every header value and prefixed token in buf
// that starts before it and does not end before it, and what to drop of one that runs to the
// end of buf.
func (w *RedactingWriter) pieceEnd(cut int) (int, continuation) {
	header, headerCont := w.r.textHeader, continueText
	if w.mode == redactJSON {
		header, headerCont = w.r.jsonHeader, continueJSON
	}
	cont := continueNone
	for {
		end := cut
		for _, m := range header.FindAllIndex(w.buf, -1) {
			start, stop, kind := m[0], m[1], headerCont
			// The value pattern stops short of an escape whose escaped byte is not held yet.
			if w.mode == redactJSON && stop == len(w.buf)-1 && w.buf[stop] == '\\' {
				stop, kind = len(w.buf), continueJSONEscape
			}
			if start < cut && stop >= cut {
				end = max(end, stop)
				if stop == len(w.buf) {
					cont = kind
				}
			}
		}
		for _, m := range w.r.prefix.FindAllIndex(w.buf, -1) {
			if m[0] < cut && m[1] >= cut {
				end = max(end, m[1])
				if m[1] == len(w.buf) && cont == continueNone {
					cont = continueToken
				}
			}
		}
		if end == cut {
			return end, cont
		}
		cut = end
	}
}

// skipContinuation returns p without the leading bytes that continue a flushed secret, and
// clears the continuation once p holds the byte that ends the secret.
func (w *RedactingWriter) skipContinuation(p []byte) []byte {
	for i, c := range p {
		switch w.cont {
		case continueNone:
			return p[i:]
		case continueToken:
			if !isTokenChar(c) {
				w.cont = continueNone
				return p[i:]
			}
		case continueText:
			if c == '\r' || c == '\n' {
				w.cont = continueNone
				return p[i:]
			}
		case continueJSONEscape:
			if c == '\n' {
				w.cont = continueNone
				return p[i:]
			}
			w.cont = continueJSON
		case continueJSON:
			switch c {
			case '\\':
				w.cont = continueJSONEscape
			case '"', '\n':
				w.cont = continueNone
				return p[i:]
			}
		}
	}
	if w.cont == continueNone {
		return p
	}
	return nil
}

// isTokenChar reports whether c is in [tokenChars], the characters of a prefixed token.
func isTokenChar(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
		strings.IndexByte("_+/=.~-", c) >= 0
}

// emit writes out to the destination, if it holds anything.
func (w *RedactingWriter) emit(out []byte) error {
	if len(out) == 0 {
		return nil
	}
	n, err := w.dst.Write(out)
	if err != nil {
		return err
	}
	if n < len(out) {
		return io.ErrShortWrite
	}
	return nil
}
