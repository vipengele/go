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
	// continueJSONValue drops the rest of a JSON header value other than a string, such as an
	// array, an object or a number, up to its end or the end of the line, by the skip state held.
	continueJSONValue
)

// RedactingWriter is an [io.Writer] that redacts every secret of a [Redaction], and of the
// baseline, from the lines written to it before they reach its destination. It reads each line by
// its shape, as [Redaction] describes, whatever the format of the handler above it, so the raw
// output of a tool or a subprocess written beside the records is redacted as well: a line that is
// a JSON document has a header value end at the closing unescaped quote, and the value of a header
// key that is an array, an object, a number, a boolean or null replaced whole by the string
// "[REDACTED]", so it stays valid JSON; any other line has a header value run to the end of the
// line.
//
// It is line-buffered: a complete line is redacted whole and written with the Write that
// completes it, and the bytes after the last newline are held. A line longer than 64 KiB is
// written in pieces: once more than that is held, the writer redacts and writes everything but
// the last 256 bytes, never splitting a UTF-8 sequence, and extends the piece over any secret
// that crosses its end. When a secret runs to the end of what is held, the piece ends with the
// redacted secret and the rest of it is dropped as it arrives, a JSON header value that is not a
// string up to the bracket that closes it or the delimiter after a scalar, following the strings
// and nesting inside it. A JSON header key is recognised in a piece only when its value has
// started there: a [RedactingWriter.Flush] that ends between the key and its value writes both
// unredacted. A header name at the start of a piece is recognised whatever byte ends the piece
// before it, so a longer name split just before a header name it ends in, such as X-Cookie split
// before Cookie, has its value redacted. A piece is therefore not a line
// on its own, and not a JSON document, but the destination's lines are the redacted lines: the
// pieces of one line concatenate to that line redacted whole.
//
// A line written in pieces, by an overflow or by [RedactingWriter.Flush], cannot be checked as a
// JSON document before its end arrives, so its shape is decided by the bytes held when its first
// piece is written, and every later piece of that line is read the same way. It is JSON when
// those bytes are a whole JSON line, an object or an array, or when they start with "{" and are
// the start of a JSON document, every token in them well formed; it is plain text otherwise, so a
// line that starts with "{" but is not JSON, such as a Go %+v dump, has a header value run to the
// end of the line. Only text that follows a complete JSON document on the same line, after a
// piece of the line has been written, is read as JSON without being JSON.
//
// A RedactingWriter is safe for concurrent use; each Write is redacted and written whole before
// the next starts.
type RedactingWriter struct {
	mu  sync.Mutex
	dst io.Writer
	r   *redactor
	// buf holds the bytes after the last newline written.
	buf []byte
	// cont is what of a flushed secret is still dropped. It is continueNone whenever buf holds
	// anything.
	cont continuation
	// skip is the state of the skip over the value continueJSONValue drops.
	skip jsonSkip
	// pieced reports that a piece of the current line has been written, and shape is then the
	// shape every later piece of that line is read as.
	pieced bool
	shape  lineShape
}

// newRedactingWriter returns a writer that redacts with r and writes to dst.
func newRedactingWriter(dst io.Writer, r *redactor) *RedactingWriter {
	return &RedactingWriter{dst: dst, r: r}
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
			out = append(out, w.redactLineEnd(line[:len(line)-1])...)
			out = append(out, '\n')
		}
		w.buf = w.buf[:copy(w.buf, w.buf[i+1:])]
	}
	if len(w.buf) > maxBuffered {
		out = w.appendPiece(out, w.windowStart())
	}
	return len(p), w.emit(out)
}

// Flush redacts and writes the bytes held after the last newline, as a piece of their line, read
// by the shape of the line: the shape its earlier pieces were read as, or, for its first piece,
// the shape of the held bytes as [RedactingWriter] describes. What it writes is redacted as it
// stands: a prefix or header name the held bytes end inside is not recognised once the rest of it
// arrives.
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

// redactLineEnd returns line, the bytes of the current line up to its newline, redacted: by the
// shape its pieces were read as when a piece of it has been written, and by its own shape
// otherwise. The next line starts unpieced.
func (w *RedactingWriter) redactLineEnd(line []byte) string {
	if w.pieced {
		w.pieced = false
		return w.r.lineAs(string(line), w.shape)
	}
	return w.r.line(string(line))
}

// windowStart returns where the window an overflow flush retains begins, moved back to the start
// of the UTF-8 sequence it would split, by at most [utf8.UTFMax]-1 bytes. It runs only once buf
// holds more than [maxBuffered] bytes, so the window starts far past the start of buf.
func (w *RedactingWriter) windowStart() int {
	cut := len(w.buf) - overflowWindow
	for i := 0; i < utf8.UTFMax-1 && !utf8.RuneStart(w.buf[cut]); i++ {
		cut--
	}
	return cut
}

// appendPiece appends to out the redacted bytes of buf up to cut, extended over every secret
// that crosses it, removes them from buf and records what of a secret that runs to the end of
// buf is still to be dropped. The first piece of a line decides the shape of the line from the
// bytes held, by [shapeOfStart].
func (w *RedactingWriter) appendPiece(out []byte, cut int) []byte {
	if !w.pieced {
		w.pieced, w.shape = true, shapeOfStart(w.buf)
	}
	end, cont := w.pieceEnd(cut)
	head := w.buf[:end]
	if cont == continueJSONEscape {
		// The trailing backslash escapes a byte not yet written; written alone it would
		// escape the next byte of the output instead.
		head = head[:len(head)-1]
	}
	out = append(out, w.r.lineAs(string(head), w.shape)...)
	w.buf = w.buf[:copy(w.buf, w.buf[end:])]
	w.cont = cont
	return out
}

// pieceEnd returns cut extended to the end of every header value and prefixed token in buf
// that starts before it and does not end before it, read as the shape of the line, and what to
// drop of one that runs to the end of buf. A JSON header value that is not a string and runs to
// the end of buf takes precedence over a string inside it, and its skip state is kept in skip.
func (w *RedactingWriter) pieceEnd(cut int) (int, continuation) {
	header, headerCont := w.r.textHeader, continueText
	if w.shape == shapeJSON {
		header, headerCont = w.r.jsonHeader, continueJSON
	}
	cont := continueNone
	for {
		end := cut
		for _, m := range header.FindAllSubmatchIndex(w.buf, -1) {
			// The secret starts at the header name; group 1 holds the byte before it.
			start, stop, kind := m[3], m[1], headerCont
			// The value pattern stops short of an escape whose escaped byte is not held yet.
			if w.shape == shapeJSON && stop == len(w.buf)-1 && w.buf[stop] == '\\' {
				stop, kind = len(w.buf), continueJSONEscape
			}
			if start < cut && stop >= cut {
				end = max(end, stop)
				if stop == len(w.buf) {
					cont = kind
				}
			}
		}
		if w.shape == shapeJSON {
			for _, v := range w.r.headerValues(string(w.buf)) {
				if v.start < cut && v.end >= cut {
					end = max(end, v.end)
					if v.open != nil {
						cont, w.skip = continueJSONValue, *v.open
					}
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
		case continueJSONValue:
			switch w.skip.next(c) {
			case skipLast:
				w.cont = continueNone
				return p[i+1:]
			case skipAfter:
				w.cont = continueNone
				return p[i:]
			case skipInside:
			}
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

// isTokenChar reports whether c is in [suffixChars], the characters of a prefixed token.
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
