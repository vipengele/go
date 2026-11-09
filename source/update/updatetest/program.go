package updatetest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// releaseProgramSource is the Go program standing in for [ReleaseBinary] where a shell script
// cannot run. Its two operands are the tag and the version document, each a quoted Go string
// literal. It answers as the script does: the document for the arguments "version --json", the tag
// for any other invocation, each followed by one "\n".
const releaseProgramSource = `package main

import (
	"fmt"
	"os"
)

const (
	tag = %s
	doc = %s
)

func main() {
	if len(os.Args) > 2 && os.Args[1] == "version" && os.Args[2] == "--json" {
		fmt.Println(doc)
		return
	}
	fmt.Println(tag)
}
`

// buildReleaseProgram writes [releaseProgramSource] for tag and goos/goarch as a module of its own
// in dir, an existing directory, builds it for goos/goarch and returns the executable's content.
// The module stands outside this one and the build runs with GOWORK=off, so it reads no go.mod or
// go.work of the repository. A failed build's error carries the go command's output.
func buildReleaseProgram(dir, tag, goos, goarch string) ([]byte, error) {
	src := fmt.Sprintf(releaseProgramSource, strconv.Quote(tag), strconv.Quote(versionDocument(tag, goos, goarch)))
	for _, f := range []struct{ name, body string }{
		{"go.mod", "module releaseprogram\n\ngo 1.21\n"},
		{"main.go", src},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o600); err != nil {
			return nil, err
		}
	}
	bin := filepath.Join(dir, "release")
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go build: %w\n%s", err, out)
	}
	return os.ReadFile(bin)
}
