package cli_test

import (
	"context"
	"encoding/json"
	"testing"

	cli "go.vipengele.org/cli/core"
	"go.vipengele.org/cli/spec"
)

// messageApp returns an App with every standard flag whose command "report" calls each message
// helper and, under --json, writes one JSON document as its data.
func messageApp() *cli.App {
	app := cli.New(identity(), cli.WithJSON(), cli.WithQuiet(), cli.WithVerbose())
	app.Add(&spec.Command{
		Name:    "report",
		Summary: "Write every kind of message",
		Run: func(_ context.Context, inv *spec.Invocation) error {
			inv.Info("fetching %s", "index")
			inv.Detail("took %dms", 12)
			inv.Success("fetched")
			inv.Warning("index is stale")
			inv.Error("%s unavailable", "mirror")
			if inv.Output.JSON() {
				return inv.Data("{\"items\":%d}\n", 3)
			}
			return nil
		},
	})
	return app
}

func TestMessagesKeepStdoutToTheData(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		wantStderr string
	}{
		{
			name: "verbose writes every message to stderr",
			flag: "--verbose",
			wantStderr: spec.GlyphInfo + " fetching index\n" +
				"took 12ms\n" +
				spec.GlyphSuccess + " fetched\n" +
				spec.GlyphWarning + " index is stale\n" +
				spec.GlyphError + " mirror unavailable\n",
		},
		{
			name:       "quiet writes only the error to stderr",
			flag:       "--quiet",
			wantStderr: spec.GlyphError + " mirror unavailable\n",
		},
	}
	const wantStdout = "{\"items\":3}\n"
	var stdouts []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runApp(t, messageApp(), "report", "--json", tt.flag)
			if got.code != cli.ExitOK {
				t.Fatalf("exit code = %d, stderr %q; want 0", got.code, got.stderr)
			}
			if got.stdout != wantStdout {
				t.Errorf("stdout = %q, want %q", got.stdout, wantStdout)
			}
			if !json.Valid([]byte(got.stdout)) {
				t.Errorf("stdout %q is not valid JSON", got.stdout)
			}
			if got.stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got.stderr, tt.wantStderr)
			}
			stdouts = append(stdouts, got.stdout)
		})
	}
	if len(stdouts) == 2 && stdouts[0] != stdouts[1] {
		t.Errorf("stdout differs between runs: %q and %q", stdouts[0], stdouts[1])
	}
}
