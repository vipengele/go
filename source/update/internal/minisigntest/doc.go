// Package minisigntest signs test fixtures in minisign's format, for the tests of
// [go.vipengele.org/update] and the programs they run.
//
// [GenerateKey] makes a [Key]: a random key id and an Ed25519 key pair. [Key.PublicKey] renders its
// public key as minisign.pub holds it, an "untrusted comment:" line followed by the key line, and
// [Key.PublicKeyLine] the key line alone. [Key.Sign] writes a signature file in the hashed "ED"
// form `minisign -S` writes, [Key.SignLegacy] one in the legacy "Ed" form `minisign -S -l` writes,
// each with the trusted comment it is given.
//
// The secret key never leaves memory: the package has no encrypted key file and no key
// derivation, and is not meant to sign anything but test fixtures.
package minisigntest
