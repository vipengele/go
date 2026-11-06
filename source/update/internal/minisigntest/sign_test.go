package minisigntest_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"go.vipengele.org/update/internal/minisign"
	"go.vipengele.org/update/internal/minisigntest"
)

func TestSignRoundTrip(t *testing.T) {
	t.Parallel()

	signer, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := minisign.ParsePublicKey(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}

	if key.ID != signer.ID || key.Line != signer.PublicKeyLine() {
		t.Fatalf("parsed key = {%s %q}, want {%s %q}", key.ID, key.Line, signer.ID, signer.PublicKeyLine())
	}

	msg := []byte("abc  tool.tar.gz\n")

	tests := []struct {
		name      string
		sig       []byte
		algorithm string
	}{
		{name: "hashed", sig: signer.Sign(msg, "timestamp:1"), algorithm: minisign.AlgorithmHashed},
		{name: "legacy", sig: signer.SignLegacy(msg, "timestamp:1"), algorithm: minisign.AlgorithmLegacy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines := strings.Split(string(tt.sig), "\n")
			if len(lines) != 5 || lines[4] != "" {
				t.Fatalf("signature file has lines %q, want four and a final newline", lines)
			}

			sig, err := minisign.ParseSignature(tt.sig)
			if err != nil {
				t.Fatal(err)
			}

			if sig.Algorithm != tt.algorithm || sig.KeyID != signer.ID || sig.TrustedComment != "timestamp:1" {
				t.Errorf("signature = %s %s %q, want %s %s %q", sig.Algorithm, sig.KeyID, sig.TrustedComment, tt.algorithm, signer.ID, "timestamp:1")
			}

			if _, err := minisign.Verify([]minisign.PublicKey{key}, msg, tt.sig); err != nil {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

func TestSignUnderChangedID(t *testing.T) {
	t.Parallel()

	signer, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := minisign.ParsePublicKey(signer.PublicKeyLine())
	if err != nil {
		t.Fatal(err)
	}

	signer.ID[0] ^= 0xff

	var unknown *minisign.UnknownKeyError
	if _, err := minisign.Verify([]minisign.PublicKey{key}, nil, signer.Sign(nil, "c")); !errors.As(err, &unknown) {
		t.Errorf("Verify = %v, want an *UnknownKeyError", err)
	}
}

func TestGenerateKeyDeterministic(t *testing.T) {
	t.Parallel()

	seed := bytes.Repeat([]byte{7}, 8+32)

	a, err := minisigntest.GenerateKey(bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}

	b, err := minisigntest.GenerateKey(bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}

	if a.PublicKey() != b.PublicKey() || !bytes.Equal(a.Sign([]byte("m"), "c"), b.Sign([]byte("m"), "c")) {
		t.Error("two keys from the same entropy differ")
	}

	if want := (minisign.KeyID{7, 7, 7, 7, 7, 7, 7, 7}); a.ID != want {
		t.Errorf("ID = %s, want %s", a.ID, want)
	}
}

func TestGenerateKeyShortEntropy(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 4, 8, 20} {
		if _, err := minisigntest.GenerateKey(bytes.NewReader(make([]byte, n))); err == nil {
			t.Errorf("GenerateKey with %d bytes of entropy succeeded, want an error", n)
		}
	}

	if _, err := minisigntest.GenerateKey(iotest.ErrReader(io.ErrUnexpectedEOF)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("GenerateKey = %v, want it to wrap the reader's error", err)
	}
}

func TestPublicKeyFormat(t *testing.T) {
	t.Parallel()

	signer, err := minisigntest.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	line := signer.PublicKeyLine()
	if len(line) != minisign.KeyLineLen || !strings.HasPrefix(line, "RW") {
		t.Errorf("PublicKeyLine = %q, want 56 characters starting RW", line)
	}

	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(raw[2:10], signer.ID[:]) || !bytes.Equal(raw[10:], signer.Public()) {
		t.Errorf("PublicKeyLine decodes to %x, want Ed, the id and the public key", raw)
	}

	want := "untrusted comment: minisign public key " + signer.ID.String() + "\n" + line + "\n"
	if got := signer.PublicKey(); got != want {
		t.Errorf("PublicKey = %q, want %q", got, want)
	}
}
