// Package minisign verifies minisign signatures, as the minisign tool writes them
// (https://jedisct1.github.io/minisign/), against a set of trusted public keys.
//
// A public key is the base64 line `minisign -G` writes as the second line of minisign.pub: the
// algorithm "Ed", an 8-byte key id and a 32-byte Ed25519 public key, 56 characters starting "RW".
// [ParsePublicKey] accepts that line alone or preceded by the file's "untrusted comment:" line, so
// an embedded minisign.pub parses as is. Surrounding whitespace is ignored.
//
// A signature file has four lines:
//
//	untrusted comment: <text>
//	base64(<algorithm> || <key id> || <signature>)
//	trusted comment: <text>
//	base64(<global signature>)
//
// The algorithm is "ED", an Ed25519 signature over the BLAKE2b-512 hash of the message, or "Ed", an
// Ed25519 signature over the message itself. The global signature is an Ed25519 signature over the
// 64-byte signature followed by the trusted comment's text, the part of its line after
// "trusted comment: ". [Verify] checks both with the trusted key whose id is the signature's. The
// untrusted comment is not authenticated and is ignored; the trusted comment is authenticated and
// not interpreted.
//
// Every failure is a typed error a caller matches with errors.As: [*MalformedKeyError] for a key
// that does not parse, [*MalformedSignatureError] for a signature file that does not parse,
// [*UnknownKeyError] for a signature whose key id no trusted key carries, and
// [*BadSignatureError] for a signature or global signature that does not verify.
//
// The package has no signing code; package go.vipengele.org/update/internal/minisigntest signs
// test fixtures.
package minisign
