package minisign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const (
	// AlgorithmHashed is the algorithm of a signature over the message's BLAKE2b-512 hash.
	AlgorithmHashed = "ED"
	// AlgorithmLegacy is the algorithm of a signature over the message itself. It is also the
	// algorithm every public key carries.
	AlgorithmLegacy = "Ed"

	// UntrustedCommentPrefix starts the first line of a signature file and of minisign.pub.
	UntrustedCommentPrefix = "untrusted comment: "
	// TrustedCommentPrefix starts the third line of a signature file. The global signature
	// covers the text after it.
	TrustedCommentPrefix = "trusted comment: "

	// KeyLineLen is the length of a public key's base64 line.
	KeyLineLen = 56

	keyIDSize = 8
	algSize   = 2
	// keySize is the decoded length of a public key line: algorithm, key id, Ed25519 key.
	keySize = algSize + keyIDSize + ed25519.PublicKeySize
	// sigSize is the decoded length of a signature line: algorithm, key id, Ed25519 signature.
	sigSize = algSize + keyIDSize + ed25519.SignatureSize
)

// KeyID is the 8-byte id that ties a signature to the key that made it, in the byte order it is
// encoded in.
type KeyID [keyIDSize]byte

// String returns the id as minisign prints it: 16 uppercase hexadecimal digits of the bytes read
// as a little-endian integer.
func (id KeyID) String() string {
	return fmt.Sprintf("%016X", binary.LittleEndian.Uint64(id[:]))
}

// PublicKey is a parsed minisign public key.
type PublicKey struct {
	// ID is the key id a signature made by this key carries.
	ID KeyID
	// Key is the Ed25519 public key.
	Key ed25519.PublicKey
	// Line is the key's 56-character base64 line, without any comment or whitespace: the form
	// `minisign -P` takes.
	Line string
}

// ParsePublicKey parses a public key entry: the base64 key line, optionally preceded by an
// "untrusted comment:" line, with surrounding whitespace ignored. It fails with a
// [*MalformedKeyError].
func ParsePublicKey(entry string) (PublicKey, error) {
	lines := strings.Split(strings.TrimSpace(entry), "\n")

	switch len(lines) {
	case 1:
	case 2:
		if !strings.HasPrefix(lines[0], strings.TrimSpace(UntrustedCommentPrefix)) {
			return PublicKey{}, &MalformedKeyError{Reason: "first of two lines is not an untrusted comment"}
		}

		lines = lines[1:]
	default:
		return PublicKey{}, &MalformedKeyError{Reason: fmt.Sprintf("%d lines, want the key line and at most a comment line before it", len(lines))}
	}

	line := strings.TrimSpace(lines[0])
	if line == "" {
		return PublicKey{}, &MalformedKeyError{Reason: "empty key line"}
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(line)
	if err != nil {
		return PublicKey{}, &MalformedKeyError{Reason: "key line is not base64", Err: err}
	}

	if len(raw) != keySize {
		return PublicKey{}, &MalformedKeyError{Reason: fmt.Sprintf("key is %d bytes, want %d", len(raw), keySize)}
	}

	if alg := string(raw[:algSize]); alg != AlgorithmLegacy {
		return PublicKey{}, &MalformedKeyError{Reason: fmt.Sprintf("key algorithm %q, want %q", alg, AlgorithmLegacy)}
	}

	key := PublicKey{Key: ed25519.PublicKey(bytes.Clone(raw[algSize+keyIDSize:])), Line: line}
	copy(key.ID[:], raw[algSize:algSize+keyIDSize])

	return key, nil
}

// Signature is a parsed minisign signature file.
type Signature struct {
	// Algorithm is [AlgorithmHashed] or [AlgorithmLegacy].
	Algorithm string
	// KeyID is the id of the key that made the signature.
	KeyID KeyID
	// Signature is the Ed25519 signature over the message, or over its BLAKE2b-512 hash.
	Signature []byte
	// TrustedComment is the trusted comment's text, without its prefix.
	TrustedComment string
	// GlobalSignature is the Ed25519 signature over Signature followed by TrustedComment.
	GlobalSignature []byte
}

// ParseSignature parses a signature file: four lines, each ending in "\n" or "\r\n", the last of
// which may end the file without one. It fails with a [*MalformedSignatureError].
func ParseSignature(data []byte) (Signature, error) {
	text := strings.TrimSuffix(string(data), "\n")

	lines := strings.Split(text, "\n")
	if len(lines) != 4 {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("%d lines, want 4", len(lines))}
	}

	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
		if strings.Contains(lines[i], "\r") {
			return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("line %d holds a carriage return", i+1)}
		}
	}

	if !strings.HasPrefix(lines[0], UntrustedCommentPrefix) {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("line 1 does not start with %q", UntrustedCommentPrefix)}
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil {
		return Signature{}, &MalformedSignatureError{Reason: "signature line is not base64", Err: err}
	}

	if len(raw) != sigSize {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("signature is %d bytes, want %d", len(raw), sigSize)}
	}

	alg := string(raw[:algSize])
	if alg != AlgorithmHashed && alg != AlgorithmLegacy {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("signature algorithm %q, want %q or %q", alg, AlgorithmHashed, AlgorithmLegacy)}
	}

	comment, ok := strings.CutPrefix(lines[2], TrustedCommentPrefix)
	if !ok {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("line 3 does not start with %q", TrustedCommentPrefix)}
	}

	global, err := base64.StdEncoding.Strict().DecodeString(lines[3])
	if err != nil {
		return Signature{}, &MalformedSignatureError{Reason: "global signature line is not base64", Err: err}
	}

	if len(global) != ed25519.SignatureSize {
		return Signature{}, &MalformedSignatureError{Reason: fmt.Sprintf("global signature is %d bytes, want %d", len(global), ed25519.SignatureSize)}
	}

	sig := Signature{
		Algorithm:       alg,
		Signature:       bytes.Clone(raw[algSize+keyIDSize:]),
		TrustedComment:  comment,
		GlobalSignature: global,
	}
	copy(sig.KeyID[:], raw[algSize:algSize+keyIDSize])

	return sig, nil
}

// Verify parses the signature file sig and verifies it over msg with the key in keys whose ID is
// the signature's. Both the signature and the global signature must verify. It returns the key
// that verified, or a [*MalformedSignatureError], an [*UnknownKeyError] when no key carries the
// signature's id, a [*BadSignatureError], or a [*MalformedKeyError] for a matching key whose Key
// is not an Ed25519 public key.
func Verify(keys []PublicKey, msg, sig []byte) (PublicKey, error) {
	parsed, err := ParseSignature(sig)
	if err != nil {
		return PublicKey{}, err
	}

	for _, key := range keys {
		if key.ID != parsed.KeyID {
			continue
		}

		if err := parsed.Verify(key, msg); err != nil {
			return PublicKey{}, err
		}

		return key, nil
	}

	return PublicKey{}, &UnknownKeyError{ID: parsed.KeyID}
}

// Verify verifies s over msg with key. It returns an [*UnknownKeyError] when key's ID is not the
// signature's, a [*MalformedKeyError] when key.Key is not an Ed25519 public key, and a
// [*BadSignatureError] when the signature or the global signature does not verify.
func (s Signature) Verify(key PublicKey, msg []byte) error {
	if key.ID != s.KeyID {
		return &UnknownKeyError{ID: s.KeyID}
	}

	if len(key.Key) != ed25519.PublicKeySize {
		return &MalformedKeyError{Reason: fmt.Sprintf("Ed25519 key is %d bytes, want %d", len(key.Key), ed25519.PublicKeySize)}
	}

	signed := msg
	if s.Algorithm == AlgorithmHashed {
		sum := blake2b.Sum512(msg)
		signed = sum[:]
	}

	if !ed25519.Verify(key.Key, signed, s.Signature) {
		return &BadSignatureError{ID: s.KeyID, Global: false}
	}

	global := make([]byte, 0, len(s.Signature)+len(s.TrustedComment))
	global = append(global, s.Signature...)
	global = append(global, s.TrustedComment...)

	if !ed25519.Verify(key.Key, global, s.GlobalSignature) {
		return &BadSignatureError{ID: s.KeyID, Global: true}
	}

	return nil
}

// MalformedKeyError reports a public key that does not parse.
type MalformedKeyError struct {
	// Reason says what is wrong with the key.
	Reason string
	// Err is the underlying cause, such as a base64 decoding error, or nil.
	Err error
}

// Error implements error.
func (e *MalformedKeyError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("minisign: malformed public key: %s: %v", e.Reason, e.Err)
	}

	return "minisign: malformed public key: " + e.Reason
}

// Unwrap returns the underlying cause.
func (e *MalformedKeyError) Unwrap() error { return e.Err }

// MalformedSignatureError reports a signature file that does not parse.
type MalformedSignatureError struct {
	// Reason says what is wrong with the signature file.
	Reason string
	// Err is the underlying cause, such as a base64 decoding error, or nil.
	Err error
}

// Error implements error.
func (e *MalformedSignatureError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("minisign: malformed signature: %s: %v", e.Reason, e.Err)
	}

	return "minisign: malformed signature: " + e.Reason
}

// Unwrap returns the underlying cause.
func (e *MalformedSignatureError) Unwrap() error { return e.Err }

// UnknownKeyError reports a signature made by a key no trusted key's id matches.
type UnknownKeyError struct {
	// ID is the signature's key id.
	ID KeyID
}

// Error implements error.
func (e *UnknownKeyError) Error() string {
	return "minisign: signature made by untrusted key " + e.ID.String()
}

// BadSignatureError reports a signature that does not verify with the key its id names.
type BadSignatureError struct {
	// ID is the key id of the signature and of the key it was checked with.
	ID KeyID
	// Global reports that the signature over the message verified and the global signature,
	// over the signature and the trusted comment, did not.
	Global bool
}

// Error implements error.
func (e *BadSignatureError) Error() string {
	if e.Global {
		return "minisign: global signature over the trusted comment does not verify with key " + e.ID.String()
	}

	return "minisign: signature does not verify with key " + e.ID.String()
}
