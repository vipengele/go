package minisigntest

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/blake2b"

	"go.vipengele.org/update/internal/minisign"
)

// Key is a minisign key pair.
type Key struct {
	// ID is the key id the public key and every signature carry. A test may change it to sign
	// under an id the verifier does not expect.
	ID minisign.KeyID
	// Private is the Ed25519 private key.
	Private ed25519.PrivateKey
}

// GenerateKey reads a key id and then an Ed25519 seed from rand, as minisign -G draws them.
func GenerateKey(rand io.Reader) (*Key, error) {
	var key Key
	if _, err := io.ReadFull(rand, key.ID[:]); err != nil {
		return nil, fmt.Errorf("minisigntest: key id: %w", err)
	}

	_, private, err := ed25519.GenerateKey(rand)
	if err != nil {
		return nil, fmt.Errorf("minisigntest: Ed25519 key: %w", err)
	}

	key.Private = private

	return &key, nil
}

// Public returns the Ed25519 public key.
func (k *Key) Public() ed25519.PublicKey {
	public, _ := k.Private.Public().(ed25519.PublicKey)

	return public
}

// PublicKeyLine returns the 56-character base64 line of the public key: "Ed", the key id and the
// Ed25519 public key.
func (k *Key) PublicKeyLine() string {
	raw := make([]byte, 0, 2+len(k.ID)+ed25519.PublicKeySize)
	raw = append(raw, minisign.AlgorithmLegacy...)
	raw = append(raw, k.ID[:]...)
	raw = append(raw, k.Public()...)

	return base64.StdEncoding.EncodeToString(raw)
}

// PublicKey returns the public key as minisign.pub holds it: the untrusted comment minisign -G
// writes, the key line and a trailing newline.
func (k *Key) PublicKey() string {
	return minisign.UntrustedCommentPrefix + "minisign public key " + k.ID.String() + "\n" +
		k.PublicKeyLine() + "\n"
}

// Sign returns the signature file of msg in the hashed "ED" form, with trustedComment as its
// trusted comment. The comment must hold no newline.
func (k *Key) Sign(msg []byte, trustedComment string) []byte {
	sum := blake2b.Sum512(msg)

	return k.sign(minisign.AlgorithmHashed, sum[:], trustedComment)
}

// SignLegacy returns the signature file of msg in the legacy "Ed" form, with trustedComment as
// its trusted comment. The comment must hold no newline.
func (k *Key) SignLegacy(msg []byte, trustedComment string) []byte {
	return k.sign(minisign.AlgorithmLegacy, msg, trustedComment)
}

// sign writes the four lines of a signature file over signed, which is the message or its hash
// as alg requires.
func (k *Key) sign(alg string, signed []byte, trustedComment string) []byte {
	sig := ed25519.Sign(k.Private, signed)
	global := ed25519.Sign(k.Private, append(append([]byte{}, sig...), trustedComment...))

	line := make([]byte, 0, 2+len(k.ID)+len(sig))
	line = append(line, alg...)
	line = append(line, k.ID[:]...)
	line = append(line, sig...)

	var b strings.Builder
	b.WriteString(minisign.UntrustedCommentPrefix + "signature from minisign secret key\n")
	b.WriteString(base64.StdEncoding.EncodeToString(line) + "\n")
	b.WriteString(minisign.TrustedCommentPrefix + trustedComment + "\n")
	b.WriteString(base64.StdEncoding.EncodeToString(global) + "\n")

	return []byte(b.String())
}
