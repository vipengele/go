package minisign_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.vipengele.org/update/internal/minisign"
	"go.vipengele.org/update/internal/minisigntest"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func generateKey(t *testing.T) *minisigntest.Key {
	t.Helper()

	key, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return key
}

func parseKey(t *testing.T, entry string) minisign.PublicKey {
	t.Helper()

	key, err := minisign.ParsePublicKey(entry)
	if err != nil {
		t.Fatal(err)
	}

	return key
}

// recode decodes a base64 line of a signature file, applies edit to its bytes and encodes it back.
func recode(t *testing.T, sig []byte, line int, edit func([]byte) []byte) []byte {
	t.Helper()

	lines := strings.Split(string(sig), "\n")

	raw, err := base64.StdEncoding.DecodeString(lines[line])
	if err != nil {
		t.Fatal(err)
	}

	lines[line] = base64.StdEncoding.EncodeToString(edit(raw))

	return []byte(strings.Join(lines, "\n"))
}

// replaceLine returns sig with its line (counted from 0) replaced by text.
func replaceLine(sig []byte, line int, text string) []byte {
	lines := strings.Split(string(sig), "\n")
	lines[line] = text

	return []byte(strings.Join(lines, "\n"))
}

func TestInteropVector(t *testing.T) {
	t.Parallel()

	pub := readTestdata(t, "minisign.pub")
	msg := readTestdata(t, "checksums.txt")

	key := parseKey(t, string(pub))
	if got, want := key.ID.String(), "1096E18282B5BA98"; got != want {
		t.Errorf("ID = %s, want %s", got, want)
	}

	if want := strings.Split(string(pub), "\n")[1]; key.Line != want {
		t.Errorf("Line = %q, want %q", key.Line, want)
	}

	tests := []struct {
		file      string
		algorithm string
		comment   string
	}{
		{file: "checksums.txt.minisig", algorithm: minisign.AlgorithmHashed, comment: "timestamp:1767225600\tfile:checksums.txt\thashed"},
		{file: "checksums.txt.legacy.minisig", algorithm: minisign.AlgorithmLegacy, comment: "timestamp:1767225600\tfile:checksums.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()

			sigFile := readTestdata(t, tt.file)

			sig, err := minisign.ParseSignature(sigFile)
			if err != nil {
				t.Fatal(err)
			}

			if sig.Algorithm != tt.algorithm || sig.KeyID != key.ID || sig.TrustedComment != tt.comment {
				t.Errorf("ParseSignature = %s %s %q, want %s %s %q", sig.Algorithm, sig.KeyID, sig.TrustedComment, tt.algorithm, key.ID, tt.comment)
			}

			got, err := minisign.Verify([]minisign.PublicKey{key}, msg, sigFile)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}

			if got.Line != key.Line {
				t.Errorf("Verify returned key %s, want %s", got.Line, key.Line)
			}

			tampered := append(bytes.Clone(msg), '\n')

			var bad *minisign.BadSignatureError
			if _, err := minisign.Verify([]minisign.PublicKey{key}, tampered, sigFile); !errors.As(err, &bad) || bad.Global {
				t.Errorf("Verify of a tampered message = %v, want a *BadSignatureError over the message", err)
			}
		})
	}
}

func TestParsePublicKey(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	line := signer.PublicKeyLine()
	comment := "untrusted comment: minisign public key " + signer.ID.String()

	if len(line) != minisign.KeyLineLen || !strings.HasPrefix(line, "RW") {
		t.Fatalf("PublicKeyLine = %q, want 56 characters starting RW", line)
	}

	tests := []struct {
		name  string
		entry string
	}{
		{name: "line only", entry: line},
		{name: "line with newline", entry: line + "\n"},
		{name: "with comment", entry: comment + "\n" + line + "\n"},
		{name: "crlf", entry: comment + "\r\n" + line + "\r\n"},
		{name: "surrounding whitespace", entry: "\n  " + comment + "\n\t" + line + "  \n\n"},
		{name: "minisigntest rendering", entry: signer.PublicKey()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, err := minisign.ParsePublicKey(tt.entry)
			if err != nil {
				t.Fatal(err)
			}

			if key.ID != signer.ID || !key.Key.Equal(signer.Public()) || key.Line != line {
				t.Errorf("ParsePublicKey = {%s %x %q}, want {%s %x %q}", key.ID, key.Key, key.Line, signer.ID, signer.Public(), line)
			}
		})
	}
}

func TestParsePublicKeyMalformed(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	line := signer.PublicKeyLine()

	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		t.Fatal(err)
	}

	hashedAlg := base64.StdEncoding.EncodeToString(append([]byte("ED"), raw[2:]...))

	tests := []struct {
		name  string
		entry string
	}{
		{name: "empty", entry: ""},
		{name: "comment only", entry: "untrusted comment: minisign public key\n"},
		{name: "comment and empty line", entry: "untrusted comment: x\n\n"},
		{name: "bad base64", entry: "RW!!" + line[4:]},
		{name: "bad padding", entry: line[:55]},
		{name: "too short", entry: base64.StdEncoding.EncodeToString(raw[:41])},
		{name: "too long", entry: base64.StdEncoding.EncodeToString(append(bytes.Clone(raw), 0))},
		{name: "wrong algorithm", entry: hashedAlg},
		{name: "first line not a comment", entry: "a comment\n" + line},
		{name: "three lines", entry: "untrusted comment: x\n" + line + "\n" + line},
		{name: "a signature file", entry: string(signer.Sign([]byte("m"), "c"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := minisign.ParsePublicKey(tt.entry)

			var malformed *minisign.MalformedKeyError
			if !errors.As(err, &malformed) {
				t.Fatalf("ParsePublicKey = %v, want a *MalformedKeyError", err)
			}
		})
	}

	_, err = minisign.ParsePublicKey("RW!!" + line[4:])

	var corrupt base64.CorruptInputError
	if !errors.As(err, &corrupt) {
		t.Errorf("ParsePublicKey of bad base64 = %v, want it to unwrap to a base64.CorruptInputError", err)
	}
}

func TestParseSignatureMalformed(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	valid := signer.Sign([]byte("message"), "comment")
	lines := strings.Split(string(valid), "\n")

	tests := []struct {
		name string
		sig  []byte
	}{
		{name: "empty", sig: nil},
		{name: "missing global signature", sig: []byte(strings.Join(lines[:3], "\n") + "\n")},
		{name: "missing untrusted comment", sig: []byte(strings.Join(lines[1:], "\n"))},
		{name: "extra line", sig: append(bytes.Clone(valid), "trailing\n"...)},
		{name: "untrusted comment prefix", sig: replaceLine(valid, 0, "comment: x")},
		{name: "trusted comment prefix", sig: replaceLine(valid, 2, "trusted_comment: comment")},
		{name: "trusted comment where untrusted belongs", sig: replaceLine(valid, 0, lines[2])},
		{name: "carriage return in a line", sig: replaceLine(valid, 2, "trusted comment: a\rb")},
		{name: "signature bad base64", sig: replaceLine(valid, 1, "!"+lines[1][1:])},
		{name: "signature too short", sig: recode(t, valid, 1, func(b []byte) []byte { return b[:73] })},
		{name: "signature too long", sig: recode(t, valid, 1, func(b []byte) []byte { return append(b, 0) })},
		{name: "signature algorithm", sig: recode(t, valid, 1, func(b []byte) []byte { b[0], b[1] = 'X', 'x'; return b })},
		{name: "signature algorithm lower case", sig: recode(t, valid, 1, func(b []byte) []byte { b[0], b[1] = 'e', 'd'; return b })},
		{name: "global signature bad base64", sig: replaceLine(valid, 3, "!"+lines[3][1:])},
		{name: "global signature too short", sig: recode(t, valid, 3, func(b []byte) []byte { return b[:63] })},
		{name: "global signature too long", sig: recode(t, valid, 3, func(b []byte) []byte { return append(b, 0) })},
		{name: "a public key", sig: []byte(signer.PublicKey())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var malformed *minisign.MalformedSignatureError

			if _, err := minisign.ParseSignature(tt.sig); !errors.As(err, &malformed) {
				t.Errorf("ParseSignature = %v, want a *MalformedSignatureError", err)
			}

			key := parseKey(t, signer.PublicKey())
			if _, err := minisign.Verify([]minisign.PublicKey{key}, []byte("message"), tt.sig); !errors.As(err, &malformed) {
				t.Errorf("Verify = %v, want a *MalformedSignatureError", err)
			}
		})
	}
}

func TestParseSignatureLineEndings(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	key := parseKey(t, signer.PublicKey())
	msg := []byte("message")
	valid := signer.Sign(msg, "a trusted comment")

	tests := []struct {
		name string
		sig  []byte
	}{
		{name: "lf", sig: valid},
		{name: "no final newline", sig: bytes.TrimSuffix(valid, []byte("\n"))},
		{name: "crlf", sig: bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sig, err := minisign.ParseSignature(tt.sig)
			if err != nil {
				t.Fatal(err)
			}

			if sig.TrustedComment != "a trusted comment" {
				t.Errorf("TrustedComment = %q, want %q", sig.TrustedComment, "a trusted comment")
			}

			if _, err := minisign.Verify([]minisign.PublicKey{key}, msg, tt.sig); err != nil {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	other := generateKey(t)
	key := parseKey(t, signer.PublicKey())
	otherKey := parseKey(t, other.PublicKey())
	msg := []byte("0123  tool_linux_amd64.tar.gz\n")

	// wrongKey carries the signer's id with another Ed25519 key.
	wrongKey := otherKey
	wrongKey.ID = key.ID

	hashed := signer.Sign(msg, "timestamp:1 file:checksums.txt hashed")
	legacy := signer.SignLegacy(msg, "timestamp:1 file:checksums.txt")

	type want int

	const (
		ok want = iota
		bad
		badGlobal
		unknown
		malformedKey
	)

	tests := []struct {
		name string
		keys []minisign.PublicKey
		msg  []byte
		sig  []byte
		want want
	}{
		{name: "hashed", keys: []minisign.PublicKey{key}, msg: msg, sig: hashed, want: ok},
		{name: "legacy", keys: []minisign.PublicKey{key}, msg: msg, sig: legacy, want: ok},
		{name: "empty message", keys: []minisign.PublicKey{key}, msg: nil, sig: signer.Sign(nil, "c"), want: ok},
		{name: "key among others", keys: []minisign.PublicKey{otherKey, key}, msg: msg, sig: hashed, want: ok},
		{name: "hashed tampered message", keys: []minisign.PublicKey{key}, msg: []byte("1123  tool_linux_amd64.tar.gz\n"), sig: hashed, want: bad},
		{name: "legacy tampered message", keys: []minisign.PublicKey{key}, msg: msg[1:], sig: legacy, want: bad},
		{name: "hashed tampered trusted comment", keys: []minisign.PublicKey{key}, msg: msg, sig: replaceLine(hashed, 2, "trusted comment: timestamp:2 file:checksums.txt hashed"), want: badGlobal},
		{name: "legacy tampered trusted comment", keys: []minisign.PublicKey{key}, msg: msg, sig: replaceLine(legacy, 2, "trusted comment: timestamp:1 file:checksums.txt "), want: badGlobal},
		{name: "tampered untrusted comment", keys: []minisign.PublicKey{key}, msg: msg, sig: replaceLine(hashed, 0, "untrusted comment: anything"), want: ok},
		{name: "hashed signed as legacy", keys: []minisign.PublicKey{key}, msg: msg, sig: recode(t, hashed, 1, func(b []byte) []byte { b[1] = 'd'; return b }), want: bad},
		{name: "legacy signed as hashed", keys: []minisign.PublicKey{key}, msg: msg, sig: recode(t, legacy, 1, func(b []byte) []byte { b[1] = 'D'; return b }), want: bad},
		{name: "global signature from another key", keys: []minisign.PublicKey{key}, msg: msg, sig: replaceLine(hashed, 3, strings.Split(string(other.Sign(msg, "timestamp:1 file:checksums.txt hashed")), "\n")[3]), want: badGlobal},
		{name: "wrong key", keys: []minisign.PublicKey{wrongKey}, msg: msg, sig: hashed, want: bad},
		{name: "unknown key id", keys: []minisign.PublicKey{otherKey}, msg: msg, sig: hashed, want: unknown},
		{name: "no keys", keys: nil, msg: msg, sig: legacy, want: unknown},
		{name: "matching key without Ed25519 key", keys: []minisign.PublicKey{{ID: key.ID}}, msg: msg, sig: hashed, want: malformedKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := minisign.Verify(tt.keys, tt.msg, tt.sig)

			var (
				badErr       *minisign.BadSignatureError
				unknownErr   *minisign.UnknownKeyError
				malformedErr *minisign.MalformedKeyError
			)

			switch tt.want {
			case ok:
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}

				if got.Line != key.Line {
					t.Errorf("Verify returned key %s, want %s", got.Line, key.Line)
				}
			case bad, badGlobal:
				if !errors.As(err, &badErr) {
					t.Fatalf("Verify = %v, want a *BadSignatureError", err)
				}

				if badErr.ID != key.ID || badErr.Global != (tt.want == badGlobal) {
					t.Errorf("BadSignatureError = %+v, want ID %s, Global %v", badErr, key.ID, tt.want == badGlobal)
				}
			case unknown:
				if !errors.As(err, &unknownErr) {
					t.Fatalf("Verify = %v, want an *UnknownKeyError", err)
				}

				if unknownErr.ID != key.ID || !strings.Contains(err.Error(), key.ID.String()) {
					t.Errorf("UnknownKeyError = %v, want it to name %s", err, key.ID)
				}
			case malformedKey:
				if !errors.As(err, &malformedErr) {
					t.Fatalf("Verify = %v, want a *MalformedKeyError", err)
				}
			}

			if err != nil && got.Key != nil {
				t.Errorf("Verify returned key %s with error %v", got.Line, err)
			}
		})
	}
}

func TestSignatureVerifyKeyIDMismatch(t *testing.T) {
	t.Parallel()

	signer := generateKey(t)
	msg := []byte("message")

	sig, err := minisign.ParseSignature(signer.Sign(msg, "c"))
	if err != nil {
		t.Fatal(err)
	}

	var unknown *minisign.UnknownKeyError
	if err := sig.Verify(parseKey(t, generateKey(t).PublicKey()), msg); !errors.As(err, &unknown) {
		t.Errorf("Signature.Verify with another key's id = %v, want an *UnknownKeyError", err)
	}
}

func TestKeyIDString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   minisign.KeyID
		want string
	}{
		{id: minisign.KeyID{0x98, 0xBA, 0xB5, 0x82, 0x82, 0xE1, 0x96, 0x10}, want: "1096E18282B5BA98"},
		{id: minisign.KeyID{0x01}, want: "0000000000000001"},
		{id: minisign.KeyID{}, want: "0000000000000000"},
	}

	for _, tt := range tests {
		if got := tt.id.String(); got != tt.want {
			t.Errorf("KeyID(%x).String() = %s, want %s", tt.id[:], got, tt.want)
		}
	}
}

func TestErrorMessages(t *testing.T) {
	t.Parallel()

	id := minisign.KeyID{0x01}
	cause := errors.New("cause")

	tests := []struct {
		err  error
		want string
	}{
		{err: &minisign.MalformedKeyError{Reason: "r"}, want: "minisign: malformed public key: r"},
		{err: &minisign.MalformedKeyError{Reason: "r", Err: cause}, want: "minisign: malformed public key: r: cause"},
		{err: &minisign.MalformedSignatureError{Reason: "r"}, want: "minisign: malformed signature: r"},
		{err: &minisign.MalformedSignatureError{Reason: "r", Err: cause}, want: "minisign: malformed signature: r: cause"},
		{err: &minisign.UnknownKeyError{ID: id}, want: "minisign: signature made by untrusted key 0000000000000001"},
		{err: &minisign.BadSignatureError{ID: id}, want: "minisign: signature does not verify with key 0000000000000001"},
		{err: &minisign.BadSignatureError{ID: id, Global: true}, want: "minisign: global signature over the trusted comment does not verify with key 0000000000000001"},
	}

	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}

	if !errors.Is(&minisign.MalformedSignatureError{Reason: "r", Err: cause}, cause) {
		t.Error("MalformedSignatureError does not unwrap to its cause")
	}

}
