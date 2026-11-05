// Command fakeminisign stands in for minisign's verify mode in the install-script tests, so a
// test never depends on the host having minisign installed. It verifies with
// go.vipengele.org/update/internal/minisign and follows minisign 0.12's command line:
//
//	minisign -V [-H] [-x sig_file] [-p pubkey_file | -P pubkey] [-o] [-q | -Q] -m file
//
// Options may come in any order, bundled (-Vq) or with the argument attached (-mfile), and each at
// most once. -x defaults to <file>.minisig and -p to ./minisign.pub. On success it exits 0 and
// prints "Signature and comment signature verified" and "Trusted comment: <comment>" to stdout
// (to stderr under -o, which then copies the file to stdout); -q prints nothing and -Q the trusted
// comment alone. A signature that does not verify, a key id other than the key's, and a legacy
// signature under -H exit 1; a usage error, an unreadable file and a malformed key or signature
// exit 2. Signing and key generation are not implemented and are usage errors.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.vipengele.org/update/internal/minisign"
)

const (
	// exitFailed is minisign's status for a signature that does not verify.
	exitFailed = 1
	// exitError is minisign's status for a usage error, an I/O error and a malformed input.
	exitError = 2

	sigSuffix     = ".minisig"
	defaultPKFile = "minisign.pub"
	version       = "minisign 0.12"

	quietNone   = 0
	quietAll    = 1
	quietPretty = 2
)

// options is the parsed command line.
type options struct {
	verify      bool
	prehashed   bool
	output      bool
	quiet       int
	messageFile string
	sigFile     string
	pkFile      string
	pubkey      string
	version     bool
}

// exitCode carries the status main exits with once the message is printed.
type exitCode int

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n\n%s", err, usage)
		return exitError
	}
	if opts.version {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if !opts.verify || opts.messageFile == "" {
		fmt.Fprint(stderr, usage)
		return exitError
	}
	if opts.sigFile == "" {
		opts.sigFile = opts.messageFile + sigSuffix
	}
	if opts.pubkey == "" && opts.pkFile == "" {
		opts.pkFile = defaultPKFile
	}
	if err := verify(opts, stdout, stderr); err != nil {
		var code exitCode
		if errors.As(err, &code) {
			return int(code)
		}
		fmt.Fprintln(stderr, err)
		return exitError
	}
	return 0
}

func (c exitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

const usage = `Usage:
minisign -V [-H] [-x sig_file] [-p pubkey_file | -P pubkey] [-o] [-q] -m file
`

// parse reads args as getopt does with minisign's verify-only option string "VhHm:oP:p:qQvx:".
func parse(args []string) (options, error) {
	var opts options
	seen := map[byte]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			// getopt stops at the first operand; minisign -V takes none.
			break
		}
		for j := 1; j < len(arg); j++ {
			flag := arg[j]
			if seen[flag] {
				return opts, fmt.Errorf("Duplicate option: -- %c", flag)
			}
			seen[flag] = true
			switch flag {
			case 'V':
				opts.verify = true
			case 'H':
				opts.prehashed = true
			case 'o':
				opts.output = true
			case 'q':
				opts.quiet = quietAll
			case 'Q':
				opts.quiet = quietPretty
			case 'v':
				opts.version = true
			case 'h':
				return opts, errors.New("help requested")
			case 'm', 'x', 'p', 'P':
				value := arg[j+1:]
				if value == "" {
					i++
					if i == len(args) {
						return opts, fmt.Errorf("option requires an argument -- %c", flag)
					}
					value = args[i]
				}
				switch flag {
				case 'm':
					opts.messageFile = value
				case 'x':
					opts.sigFile = value
				case 'p':
					opts.pkFile = value
				case 'P':
					opts.pubkey = value
				}
				j = len(arg)
			default:
				return opts, fmt.Errorf("invalid option -- %c", flag)
			}
		}
	}
	return opts, nil
}

// verify checks the signature in opts.sigFile over opts.messageFile and prints what minisign
// prints. A failure it has already reported is an exitCode.
func verify(opts options, stdout, stderr io.Writer) error {
	key, err := loadKey(opts)
	if err != nil {
		return err
	}
	sigData, err := os.ReadFile(opts.sigFile)
	if err != nil {
		return err
	}
	sig, err := minisign.ParseSignature(sigData)
	if err != nil {
		return err
	}
	if sig.Algorithm == minisign.AlgorithmLegacy && opts.prehashed {
		if opts.quiet == quietNone {
			fmt.Fprintln(stderr, "Legacy (non-prehashed) signature found")
		}
		return exitCode(exitFailed)
	}
	msg, err := os.ReadFile(opts.messageFile)
	if err != nil {
		return err
	}
	if sig.KeyID != key.ID {
		fmt.Fprintf(stderr, "Signature key id in %s is %s\nbut the key id in the public key is %s\n", opts.sigFile, sig.KeyID, key.ID)
		return exitCode(exitFailed)
	}
	if err := sig.Verify(key, msg); err != nil {
		var bad *minisign.BadSignatureError
		if !errors.As(err, &bad) {
			return err
		}
		if opts.quiet == quietNone {
			if bad.Global {
				fmt.Fprintln(stderr, "Comment signature verification failed")
			} else {
				fmt.Fprintln(stderr, "Signature verification failed")
			}
		}
		return exitCode(exitFailed)
	}

	info := stdout
	if opts.output {
		info = stderr
	}
	switch opts.quiet {
	case quietNone:
		fmt.Fprintf(info, "Signature and comment signature verified\nTrusted comment: %s\n", sig.TrustedComment)
	case quietPretty:
		fmt.Fprintln(info, sig.TrustedComment)
	}
	if opts.output {
		if _, err := stdout.Write(msg); err != nil {
			return err
		}
	}
	return nil
}

// loadKey parses the key given by -P, or else the key line of the file -p names: its second line,
// after the untrusted comment.
func loadKey(opts options) (minisign.PublicKey, error) {
	if opts.pubkey != "" {
		return minisign.ParsePublicKey(opts.pubkey)
	}
	data, err := os.ReadFile(opts.pkFile)
	if err != nil {
		return minisign.PublicKey{}, err
	}
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return minisign.PublicKey{}, errors.New("Error while loading the public key file")
	}
	return minisign.ParsePublicKey(lines[1])
}
