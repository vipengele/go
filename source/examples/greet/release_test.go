package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"go.vipengele.org/update/goreleaser"
	"go.yaml.in/yaml/v3"
)

// The flag is registered in init and read back by name: a package-level variable holding it would
// be mutable global state.
func init() {
	flag.Bool("update", false, "rewrite .goreleaser.yaml")
}

// goreleaserOptions are the options the tool's .goreleaser.yaml is rendered with: the main package
// is the project directory, and every other option takes its default.
func goreleaserOptions() goreleaser.Options {
	return goreleaser.Options{Main: "./"}
}

// TestReleaseGoreleaserConfig checks that the committed .goreleaser.yaml is what goreleaser.Render
// of the required update release emits for the tool's update.Config, byte for byte, so the
// archives and checksums a release publishes are the ones "update" looks for.
func TestReleaseGoreleaserConfig(t *testing.T) {
	got, err := goreleaser.Render(updateConfig(prodDeps()), goreleaserOptions())
	if err != nil {
		t.Fatal(err)
	}

	const path = ".goreleaser.yaml"
	if flag.Lookup("update").Value.String() == "true" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from the rendered config; run GOWORK=off go test -run TestReleaseGoreleaserConfig . -update\nrendered:\n%s\ncommitted:\n%s", path, got, want)
	}
}

// releaseToolPattern is the reusable workflow a tool's release job calls, pinned at a full commit
// SHA, and releaseCommentPattern the comment beside it naming the update tag the SHA is.
var (
	releaseToolPattern    = regexp.MustCompile(`^vipengele/go/\.github/workflows/release-tool\.yml@[0-9a-f]{40}$`)
	releaseCommentPattern = regexp.MustCompile(`^# source/update/(v\S+)$`)
)

// callerWorkflow is the part of a tool's release workflow the test checks.
type callerWorkflow struct {
	On struct {
		Push struct {
			Tags []string `yaml:"tags"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]callerJob `yaml:"jobs"`
}

// callerJob is a job that calls a reusable workflow.
type callerJob struct {
	Permissions map[string]string `yaml:"permissions"`
	Uses        string            `yaml:"uses"`
	With        map[string]any    `yaml:"with"`
	Secrets     map[string]string `yaml:"secrets"`
}

// calledWorkflow is the workflow_call interface of release-tool.yml.
type calledWorkflow struct {
	On struct {
		WorkflowCall struct {
			Inputs  map[string]any `yaml:"inputs"`
			Secrets map[string]any `yaml:"secrets"`
		} `yaml:"workflow_call"`
	} `yaml:"on"`
}

// TestReleaseWorkflow checks that release.yml, the workflow a tool runs on a tag push, calls
// release-tool.yml pinned at a commit SHA whose comment names the update release go.mod requires,
// grants the job the contents: write the publish job needs, passes the signing key, and names the
// environment that holds it.
func TestReleaseWorkflow(t *testing.T) {
	data, err := os.ReadFile("release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf callerWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("release.yml: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("release.yml: %v", err)
	}

	if !slices.Contains(wf.On.Push.Tags, "v*") {
		t.Errorf("on.push.tags = %q, want it to hold %q", wf.On.Push.Tags, "v*")
	}
	if len(wf.Jobs) != 1 {
		t.Fatalf("release.yml holds %d jobs, want 1", len(wf.Jobs))
	}
	var name string
	for name = range wf.Jobs {
	}
	job := wf.Jobs[name]

	if !releaseToolPattern.MatchString(job.Uses) {
		t.Errorf("jobs.%s.uses = %q, want vipengele/go/.github/workflows/release-tool.yml@<40-hex SHA>", name, job.Uses)
	}
	uses := mappingValue(mappingValue(mappingValue(doc.Content[0], "jobs"), name), "uses")
	if uses == nil {
		t.Fatalf("jobs.%s.uses not found", name)
	}
	want := requiredUpdateVersion(t)
	if m := releaseCommentPattern.FindStringSubmatch(uses.LineComment); m == nil {
		t.Errorf("jobs.%s.uses carries the comment %q, want %q", name, uses.LineComment, "# source/update/"+want)
	} else if m[1] != want {
		t.Errorf("jobs.%s.uses names source/update/%s, but go.mod requires go.vipengele.org/update %s; move the SHA and the comment to the tag source/update/%s", name, m[1], want, want)
	}

	// GitHub checks the permissions of every job of the called workflow when the run starts, so a
	// caller that grants less than the publish job's contents: write fails before building.
	if got := job.Permissions["contents"]; got != "write" {
		t.Errorf("jobs.%s.permissions.contents = %q, want %q", name, got, "write")
	}
	if got := job.Secrets["MINISIGN_KEY"]; got == "" {
		t.Errorf("jobs.%s.secrets passes no MINISIGN_KEY", name)
	}
	if env, _ := job.With["environment"].(string); env == "" {
		t.Errorf("jobs.%s.with.environment = %v, want the name of the environment holding MINISIGN_KEY", name, job.With["environment"])
	}

	// The repository's own release-tool.yml stands in for the copy at the pinned SHA: the two
	// differ only when the workflow has changed since the update release, and an input added
	// since then would pass here and fail in the tool's run.
	calledData, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "release-tool.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var called calledWorkflow
	if err := yaml.Unmarshal(calledData, &called); err != nil {
		t.Fatalf("release-tool.yml: %v", err)
	}
	for key := range job.With {
		if _, ok := called.On.WorkflowCall.Inputs[key]; !ok {
			t.Errorf("jobs.%s.with.%s is not an input of release-tool.yml", name, key)
		}
	}
	for key := range job.Secrets {
		if _, ok := called.On.WorkflowCall.Secrets[key]; !ok {
			t.Errorf("jobs.%s.secrets.%s is not a secret of release-tool.yml", name, key)
		}
	}
}

// mappingValue returns the value of key in the mapping node n, or nil when n is not a mapping or
// holds no key.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// requiredUpdateVersion returns the version of go.vipengele.org/update that go.mod requires.
func requiredUpdateVersion(t *testing.T) string {
	t.Helper()
	version, ok := requiredVersions(t)["go.vipengele.org/update"]
	if !ok {
		t.Fatal("go.mod does not require go.vipengele.org/update")
	}
	return version
}
