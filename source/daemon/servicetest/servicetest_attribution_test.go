package servicetest

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"go.vipengele.org/daemon/service"
)

// attributionEnv names the entry point TestAttributionChild calls when the
// test binary is re-executed as the child of
// TestFailuresAreAttributedToTheCaller.
const attributionEnv = "SERVICETEST_ATTRIBUTION"

// attributionFile is this file, where every failure the child reports is
// attributed when the entry point and the functions it calls are helpers.
const attributionFile = "servicetest_attribution_test.go"

// TestAttributionChild calls the entry point attributionEnv names, which
// fails the test.
func TestAttributionChild(t *testing.T) {
	switch os.Getenv(attributionEnv) {
	case "Run":
		Run(t, Target{})
	case "run":
		run(t, harness{goBuild: okBuild}, Target{})
	case "runScope":
		runScope(t, harness{r: &fakeRunner{}}, service.Scope(5), "", nil)
	default:
		t.Skip("runs only as the child of TestFailuresAreAttributedToTheCaller")
	}
}

// TestFailuresAreAttributedToTheCaller runs each entry point that takes a
// *testing.T in a child test binary until it fails, and checks that the
// failure names the file of the test that called it.
func TestFailuresAreAttributedToTheCaller(t *testing.T) {
	cases := []struct {
		entry string
		env   []string
		want  string
	}{
		{"Run", []string{enableEnv + "=1", podmanEnv + "=doas podman"}, `servicetest: ` + podmanEnv + ` is "doas podman"`},
		{"run", nil, "servicetest: Target.Binary is empty"},
		{"runScope", nil, "servicetest: scope 5 is neither ScopeSystem nor ScopeUser"},
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestAttributionChild$", "-test.v") // #nosec G204 -- the program is this test binary, re-executed with constant flags; no shell is involved
			cmd.Env = append(os.Environ(), attributionEnv+"="+tc.entry)
			cmd.Env = append(cmd.Env, tc.env...)
			out, err := cmd.CombinedOutput()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("child = %v, want it to fail; output:\n%s", err, out)
			}
			re := regexp.MustCompile(`(?m)^\s+(\S+):\d+: ` + regexp.QuoteMeta(tc.want))
			m := re.FindSubmatch(out)
			if m == nil {
				t.Fatalf("child output lacks %q:\n%s", tc.want, out)
			}
			if file := string(m[1]); file != attributionFile {
				t.Errorf("failure attributed to %s, want %s; output:\n%s", file, attributionFile, strings.TrimSpace(string(out)))
			}
		})
	}
}
