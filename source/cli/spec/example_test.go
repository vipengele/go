package spec_test

import (
	"fmt"

	"go.vipengele.org/cli/spec"
)

func ExampleIdentity() {
	id := spec.Identity{
		Name:      "atk",
		Owner:     "pedromvgomes",
		Repo:      "agentic-toolkit",
		EnvPrefix: "ATK",
		DirName:   "agentic-toolkit",
	}
	if err := id.Validate(); err != nil {
		fmt.Println(err)
		return
	}

	noColor, _ := id.EnvVar("NO_COLOR")
	cache, _ := id.Subdir("cache")
	fmt.Println(id.Slug())
	fmt.Println(noColor)
	fmt.Println(cache)
	// Output:
	// pedromvgomes/agentic-toolkit
	// ATK_NO_COLOR
	// agentic-toolkit/cache
}
