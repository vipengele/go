package speccheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// updateEnv is the environment variable that, set to "1", makes AssertJSONKeys write its golden
// file instead of comparing against it.
const updateEnv = "SPECCHECK_UPDATE"

// AssertJSONKeys fails t unless the key paths of v, marshalled with encoding/json, are exactly
// those listed in the golden file at goldenPath. With updateEnv set to "1" it writes the golden
// file from v and compares nothing. A value that does not marshal and a golden file that cannot be
// read or written fail t as well.
func AssertJSONKeys(t testing.TB, v any, goldenPath string) {
	t.Helper()

	got, err := keyPaths(v)
	if err != nil {
		t.Fatalf("speccheck: marshal %T: %v", v, err)
	}

	if os.Getenv(updateEnv) == "1" {
		if err := writeGolden(goldenPath, got); err != nil {
			t.Fatalf("speccheck: write golden file: %v", err)
		}
		return
	}

	content, err := os.ReadFile(goldenPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("speccheck: golden file %s does not exist; run the test with %s=1 to create it", goldenPath, updateEnv)
	}
	if err != nil {
		t.Fatalf("speccheck: read golden file: %v", err)
	}

	extra, missing := difference(got, parseGolden(content))
	if len(extra) == 0 && len(missing) == 0 {
		return
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "speccheck: JSON keys of %T differ from %s", v, goldenPath)
	writeSection(&msg, "extra (in the value, not in the golden file):", "+ ", extra)
	writeSection(&msg, "missing (in the golden file, not in the value):", "- ", missing)
	msg.WriteString("\nrenaming, removing or retyping a key increments \"schema\"; run the test with " +
		updateEnv + "=1 to rewrite the golden file")
	t.Fatalf("%s", msg.String())
}

// keyPaths returns the sorted, de-duplicated key paths of v's JSON encoding.
func keyPaths(v any) ([]string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	collect(set, "", decoded)
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths, nil
}

// collect adds to set the leaf paths of value, which sits at path prefix. The root value has the
// empty prefix, and a scalar at the root has no key, so it contributes nothing.
func collect(set map[string]struct{}, prefix string, value any) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			add(set, prefix)
			return
		}
		for key, child := range v {
			if prefix == "" {
				collect(set, key, child)
			} else {
				collect(set, prefix+"."+key, child)
			}
		}
	case []any:
		elem := prefix + "[]"
		if len(v) == 0 {
			add(set, elem)
			return
		}
		for _, child := range v {
			collect(set, elem, child)
		}
	default:
		add(set, prefix)
	}
}

func add(set map[string]struct{}, path string) {
	if path != "" {
		set[path] = struct{}{}
	}
}

// parseGolden returns the key paths listed in a golden file, sorted and without blank lines.
func parseGolden(content []byte) []string {
	var paths []string
	for line := range strings.Lines(string(content)) {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func writeGolden(path string, paths []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var content strings.Builder
	for _, p := range paths {
		content.WriteString(p + "\n")
	}
	return os.WriteFile(path, []byte(content.String()), 0o644)
}

// difference returns the paths in got and not in want, and those in want and not in got. Both
// inputs are sorted.
func difference(got, want []string) (extra, missing []string) {
	for _, p := range got {
		if _, found := slices.BinarySearch(want, p); !found {
			extra = append(extra, p)
		}
	}
	for _, p := range want {
		if _, found := slices.BinarySearch(got, p); !found {
			missing = append(missing, p)
		}
	}
	return extra, missing
}

func writeSection(b *strings.Builder, title, marker string, paths []string) {
	if len(paths) == 0 {
		return
	}
	b.WriteString("\n" + title)
	for _, p := range paths {
		b.WriteString("\n  " + marker + p)
	}
}
