package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

var (
	snippetMarker = regexp.MustCompile(`^\s*<!--\s*snippet:\s*(\S+?)(?:#(\S+))?\s*-->\s*$`)
	moduleVersion = regexp.MustCompile(`go\.vipengele\.org/([A-Za-z0-9._/-]+)@(v[0-9][0-9A-Za-z.+-]*)`)
)

// guideBlock is a fenced code block of a guide.
type guideBlock struct {
	line   int // 1-based line of the opening fence
	lang   string
	text   string
	marker *guideMarker
}

// guideMarker is the snippet comment that precedes a block: the file, and the region within it
// (empty for the whole file).
type guideMarker struct {
	file, region string
}

// checkGuide checks the guide at guidePath against the sources it quotes, resolved relative to
// baseDir, and returns one error per violation; none means the guide is current.
//
// Every go and yaml block is preceded, with only blank lines between, by
// "<!-- snippet: file#region -->" or "<!-- snippet: file -->" and equals that region or file. A
// region lies between "// snippet:start name" and the next "// snippet:end" ("#" instead of "//"
// in YAML); the marker lines are excluded, leading and trailing blank lines are dropped and the
// common indentation is removed from both sides. A whole file is compared with its trailing
// newlines dropped. The fence language is case-insensitive and only its first word counts: "golang"
// is go and "yml" is yaml. Blocks of bash, sh and shell are not compared; every
// go.vipengele.org/<project>@<version> in them must equal versions[go.vipengele.org/<project>].
// Blocks of any other language, or none, are ignored. A missing guide is an error.
func checkGuide(guidePath, baseDir string, versions map[string]string) []error {
	data, err := os.ReadFile(guidePath)
	if err != nil {
		return []error{fmt.Errorf("guide: %w", err)}
	}
	var errs []error
	for _, b := range parseGuide(string(data)) {
		where := fmt.Sprintf("%s:%d", guidePath, b.line)
		switch normalizeLang(b.lang) {
		case "go", "yaml":
			if b.marker == nil {
				errs = append(errs, fmt.Errorf("%s: %s block has no <!-- snippet: file#region --> marker", where, b.lang))
				continue
			}
			if err := checkBlock(b, baseDir); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", where, err))
			}
		case "bash", "sh", "shell":
			for _, m := range moduleVersion.FindAllStringSubmatch(b.text, -1) {
				mod := "go.vipengele.org/" + m[1]
				have, ok := versions[mod]
				switch {
				case !ok:
					errs = append(errs, fmt.Errorf("%s: %s is not required by go.mod", where, mod))
				case have != m[2]:
					errs = append(errs, fmt.Errorf("%s: %s@%s, but go.mod requires %s", where, mod, m[2], have))
				}
			}
		}
	}
	return errs
}

// normalizeLang returns the canonical lower-case name of a fence language.
func normalizeLang(lang string) string {
	lang = strings.ToLower(lang)
	switch lang {
	case "golang":
		return "go"
	case "yml":
		return "yaml"
	}
	return lang
}

// parseGuide returns the fenced blocks of a markdown guide with the snippet marker each is
// preceded by. A marker applies to the next fenced block only, and only across blank lines.
func parseGuide(src string) []guideBlock {
	var (
		blocks  []guideBlock
		pending *guideMarker
		cur     *guideBlock
		body    []string
	)
	for i, line := range strings.Split(src, "\n") {
		if cur != nil {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				cur.text = strings.Join(body, "\n")
				blocks = append(blocks, *cur)
				cur, body = nil, nil
				continue
			}
			body = append(body, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			lang, _, _ := strings.Cut(strings.TrimSpace(strings.TrimLeft(trimmed, "`")), " ")
			cur = &guideBlock{line: i + 1, lang: lang, marker: pending}
			pending = nil
		case snippetMarker.MatchString(line):
			m := snippetMarker.FindStringSubmatch(line)
			pending = &guideMarker{file: m[1], region: m[2]}
		case trimmed == "":
		default:
			pending = nil
		}
	}
	return blocks
}

// checkBlock compares a block with the region or file its marker names.
func checkBlock(b guideBlock, baseDir string) error {
	m := b.marker
	data, err := os.ReadFile(filepath.Join(baseDir, m.file))
	if err != nil {
		return fmt.Errorf("snippet file: %w", err)
	}
	want := strings.TrimRight(string(data), "\n")
	if m.region != "" {
		regions, err := parseRegions(m.file, string(data))
		if err != nil {
			return err
		}
		r, ok := regions[m.region]
		if !ok {
			return fmt.Errorf("%s has no region %q", m.file, m.region)
		}
		want = r
	}
	got := normalizeRegion(b.text)
	if m.region != "" {
		want = normalizeRegion(want)
	} else {
		got = strings.TrimRight(b.text, "\n")
	}
	if got != want {
		return fmt.Errorf("block does not match %s:\n--- guide ---\n%s\n--- source ---\n%s", describe(m), got, want)
	}
	return nil
}

func describe(m *guideMarker) string {
	if m.region == "" {
		return m.file
	}
	return m.file + "#" + m.region
}

// parseRegions returns the regions of a source file by name. The comment leader is "#" for
// .yml and .yaml files and "//" for any other.
func parseRegions(file, src string) (map[string]string, error) {
	leader := "//"
	if ext := filepath.Ext(file); ext == ".yml" || ext == ".yaml" {
		leader = "#"
	}
	var (
		regions = map[string]string{}
		name    string
		body    []string
	)
	for i, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, leader+" snippet:start "):
			if name != "" {
				return nil, fmt.Errorf("%s:%d: region %q is not terminated before the next start", file, i+1, name)
			}
			name = strings.TrimSpace(strings.TrimPrefix(t, leader+" snippet:start "))
			if _, dup := regions[name]; dup {
				return nil, fmt.Errorf("%s:%d: duplicate region %q", file, i+1, name)
			}
			body = nil
		case t == leader+" snippet:end":
			if name == "" {
				return nil, fmt.Errorf("%s:%d: snippet:end without a start", file, i+1)
			}
			regions[name] = strings.Join(body, "\n")
			name = ""
		case name != "":
			body = append(body, line)
		}
	}
	if name != "" {
		return nil, fmt.Errorf("%s: region %q is not terminated", file, name)
	}
	return regions, nil
}

// normalizeRegion drops leading and trailing blank lines and the indentation every non-blank line
// shares.
func normalizeRegion(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if len(l) >= indent && indent > 0 {
			lines[i] = l[indent:]
		} else if strings.TrimSpace(l) == "" {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// requiredVersions returns the module versions the example's go.mod requires, by module path: the
// versions a consumer's "go get" resolves. go.mod is read rather than the build information of the
// test binary, which reports "(devel)" for a module the workspace resolves from the working tree.
// A replace directive fails the test: the example requires releases.
func requiredVersions(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	for _, r := range f.Replace {
		t.Errorf("go.mod replaces %s with %s; the example requires releases", r.Old.Path, r.New.Path)
		replaced = true
	}
	versions := map[string]string{}
	for _, r := range f.Require {
		versions[r.Mod.Path] = r.Mod.Version
	}
	if replaced {
		t.FailNow()
	}
	return versions
}

// TestGuide checks the consumer guide, docs/guide/consumer.md, against the example's sources, and
// every go.vipengele.org/<module>@<version> in its commands against the version go.mod requires.
func TestGuide(t *testing.T) {
	guide := filepath.Join("..", "..", "..", "docs", "guide", "consumer.md")
	for _, err := range checkGuide(guide, ".", requiredVersions(t)) {
		t.Error(err)
	}
}

// TestGuideChecker runs checkGuide over the cases in testdata/guide: a pass case yields no error,
// a fail case an error containing the expected text.
func TestGuideChecker(t *testing.T) {
	versions := map[string]string{"go.vipengele.org/update": "v0.1.0"}
	tests := []struct {
		name string
		want string // empty: the guide passes
	}{
		{"match", ""},
		{"mismatch", "does not match"},
		{"indented", ""},
		{"wholefile", ""},
		{"yaml", ""},
		{"ymlmarker", ""},
		{"ymlnomarker", "has no <!-- snippet"},
		{"nomarker", "has no <!-- snippet"},
		{"unknownregion", `has no region "missing"`},
		{"unknownfile", "snippet file"},
		{"duplicate", `duplicate region "a"`},
		{"unterminated", "is not terminated"},
		{"strayend", "snippet:end without a start"},
		{"bashexempt", ""},
		{"bashversion", "go.mod requires v0.1.0"},
		{"bashunknown", "is not required by go.mod"},
		{"noguide", "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join("testdata", "guide", tt.name)
			errs := checkGuide(filepath.Join(dir, "guide.md"), dir, versions)
			if tt.want == "" {
				if len(errs) != 0 {
					t.Fatalf("checkGuide = %v, want none", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("checkGuide = none, want an error containing %q", tt.want)
			}
			var all []string
			for _, err := range errs {
				all = append(all, err.Error())
			}
			if joined := strings.Join(all, "\n"); !strings.Contains(joined, tt.want) {
				t.Fatalf("checkGuide = %q, want an error containing %q", joined, tt.want)
			}
		})
	}
}
