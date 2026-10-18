package logging_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"go.vipengele.org/cli/core/logging"
	"go.vipengele.org/cli/spec"
)

func ExampleSetup() {
	prev := slog.Default()
	defer slog.SetDefault(prev)

	var out strings.Builder
	res, err := logging.Setup(logging.Options{
		Format:    spec.FormatJSON,
		Writer:    &out,
		Redaction: logging.Redaction{Prefixes: []string{"ghp_"}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// A record carries its time, so only the redacted attributes are printed.
	res.Logger.Info("cloning", "token", "ghp_abc123", "Authorization", "Bearer abc123")
	var rec map[string]any
	if err := json.Unmarshal([]byte(out.String()), &rec); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(rec["token"], rec["Authorization"])

	// Text written to the writer directly is redacted in the same format.
	out.Reset()
	if _, err := fmt.Fprintln(res.Writer, `{"msg":"raw","Cookie":"session=abc"}`); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(out.String())
	// Output:
	// ghp_[REDACTED] [REDACTED]
	// {"msg":"raw","Cookie":"[REDACTED]"}
}

func ExampleParseLevel() {
	level, err := logging.ParseLevel("WARN")
	fmt.Println(level, err)

	_, err = logging.ParseLevel("loud")
	fmt.Println(err)
	// Output:
	// WARN <nil>
	// unknown log level "loud": want debug, info, warn or error
}
