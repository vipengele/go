// Command releasefixture is the tool the release workflow's self-test builds with GoReleaser. It
// prints the version the linker sets into version, "dev" when none is set.
package main

import "fmt"

// version, commit and date are set by the -X flags of the rendered .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	fmt.Println(version, commit, date)
}
