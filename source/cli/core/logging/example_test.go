package logging_test

import (
	"fmt"

	"go.vipengele.org/cli/core/logging"
)

func ExampleParseLevel() {
	level, err := logging.ParseLevel("WARN")
	fmt.Println(level, err)

	_, err = logging.ParseLevel("loud")
	fmt.Println(err)
	// Output:
	// WARN <nil>
	// unknown log level "loud": want debug, info, warn or error
}
