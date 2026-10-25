package logtest_test

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"go.vipengele.org/cli/core/logging/logtest"
)

func ExampleRecorder() {
	rec := logtest.New()
	logger := slog.New(rec)

	logger.Info("request served", slog.Group("http", slog.Int("status", 200)), "path", "/health")

	for _, r := range rec.Records() {
		fmt.Println(r.Level, r.Message)
		for _, key := range slices.Sorted(maps.Keys(r.Attrs)) {
			fmt.Printf("%s=%v\n", key, r.Attrs[key])
		}
	}
	// Output:
	// INFO request served
	// http.status=200
	// path=/health
}
