package updatetest

import "fmt"

// Target is one route of a [Server]: a route a fault is attached to and its requests are counted
// by.
type Target int

const (
	// TargetLatest is the latest-release web page, GET /<owner>/<repo>/releases/latest.
	TargetLatest Target = iota + 1
	// TargetLatestAPI is the REST API latest release, GET /repos/<owner>/<repo>/releases/latest.
	TargetLatestAPI
	// TargetArchive is the release's archive, GET /<owner>/<repo>/releases/download/<tag>/<archive>.
	TargetArchive
	// TargetChecksums is the release's checksums file, GET
	// /<owner>/<repo>/releases/download/<tag>/<checksums>.
	TargetChecksums
	// TargetSignature is the minisign signature of the checksums file, GET
	// /<owner>/<repo>/releases/download/<tag>/<checksums>.minisig.
	TargetSignature
)

// String returns the name of the constant t is, or "Target(<n>)" for any other value.
func (t Target) String() string {
	switch t {
	case TargetLatest:
		return "TargetLatest"
	case TargetLatestAPI:
		return "TargetLatestAPI"
	case TargetArchive:
		return "TargetArchive"
	case TargetChecksums:
		return "TargetChecksums"
	case TargetSignature:
		return "TargetSignature"
	default:
		return fmt.Sprintf("Target(%d)", int(t))
	}
}

// Option configures a fault or a serving mode of the [Server] [NewServer] starts.
type Option func(*options)

// options is what the [Option]s of one [NewServer] call set.
type options struct {
	apiOnly      bool
	badChecksum  bool
	badSignature bool
	unknownKey   bool
	tampered     bool
	missing      map[string]bool
	slow         map[Target]<-chan struct{}
}

// APIOnly has the latest-release web page answer 404 Not Found instead of redirecting to the
// release's tag page. A GitHubSource then finds no release by the redirect and reads the release,
// with its asset list, from the REST API. Without APIOnly both routes serve the release, and a
// GitHubSource takes it from the redirect, with no asset list.
func APIOnly() Option {
	return func(o *options) {
		o.apiOnly = true
	}
}

// BadChecksum has the checksums file list, for the archive's name, a digest that is not the
// digest of the archive bytes served.
func BadChecksum() Option {
	return func(o *options) {
		o.badChecksum = true
	}
}

// BadSignature has the server serve a signature that is well formed and carries the server's key
// id, but does not verify under [Server.PublicKey].
func BadSignature() Option {
	return func(o *options) {
		o.badSignature = true
	}
}

// UnknownKey has the server serve a valid signature made by another key, with another key id, so
// a verifier holding only [Server.PublicKey] finds no key for it.
func UnknownKey() Option {
	return func(o *options) {
		o.unknownKey = true
	}
}

// Tampered has the server serve an archive and a checksums file that differ from the ones the
// signature was made over, consistently: the checksums file lists the served archive's digest, so
// the checksum matches, and the signature does not verify.
func Tampered() Option {
	return func(o *options) {
		o.tampered = true
	}
}

// MissingAsset removes the asset called name from the release: its download URL answers
// 404 Not Found, and the REST API's asset list does not name it. A missing archive is still listed
// in the checksums file. name is the archive's, the checksums file's or the signature's name, the
// checksums file's name and ".minisig"; any other name has no effect.
func MissingAsset(name string) Option {
	return func(o *options) {
		if o.missing == nil {
			o.missing = map[string]bool{}
		}
		o.missing[name] = true
	}
}

// Slow has every request to target wait, before anything of its response is written, until gate
// is closed or the request's context ends. A request whose context ends gets no response: its
// connection is closed. [Server.Blocked] reports when a request starts waiting. The server's
// cleanup also releases a waiting request, with no response, so a gate the test never closes does
// not hang it.
func Slow(target Target, gate <-chan struct{}) Option {
	return func(o *options) {
		if o.slow == nil {
			o.slow = map[Target]<-chan struct{}{}
		}
		o.slow[target] = gate
	}
}
