package speccheck

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTB records what AssertJSONKeys reports. It embeds testing.TB to satisfy the interface's
// unexported method; any method it does not override panics on the nil embedded value. Fatalf
// ends the calling goroutine with runtime.Goexit, as the real one does, so run must call the
// helper on a goroutine of its own.
type fakeTB struct {
	testing.TB

	helpers int
	fatals  []string
	errors  []string
}

func (f *fakeTB) Helper() { f.helpers++ }

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func (f *fakeTB) failed() bool { return len(f.fatals) > 0 || len(f.errors) > 0 }

func (f *fakeTB) message() string { return strings.Join(append(f.fatals, f.errors...), "\n") }

// run calls AssertJSONKeys with a fake TB on its own goroutine and waits for it to return or
// to stop at Fatalf.
func run(v any, goldenPath string) *fakeTB {
	f := &fakeTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		AssertJSONKeys(f, v, goldenPath)
	}()
	<-done
	return f
}

type owner struct {
	Name string `json:"name"`
}

type meta struct {
	Created string `json:"created"`
	Owner   owner  `json:"owner"`
}

type item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type record struct {
	Schema int      `json:"schema"`
	Name   string   `json:"name"`
	Meta   meta     `json:"meta"`
	Items  []item   `json:"items"`
	Tags   []string `json:"tags"`
	None   []item   `json:"none"`
	Empty  struct{} `json:"empty"`
}

// noUpdate clears the update switch, so a developer running the suite with it set does not
// rewrite this package's fixtures.
func noUpdate(t *testing.T) {
	t.Helper()
	t.Setenv(updateEnv, "")
}

func TestAssertJSONKeysPassesWhenKeysMatchGolden(t *testing.T) {
	noUpdate(t)
	v := record{
		Schema: 1,
		Items:  []item{{ID: 1, Name: "a"}},
		Tags:   []string{"x"},
		None:   []item{},
	}

	f := run(v, filepath.Join("testdata", "record.golden"))

	if f.failed() {
		t.Fatalf("AssertJSONKeys failed on matching keys:\n%s", f.message())
	}
	if f.helpers == 0 {
		t.Error("AssertJSONKeys did not call Helper")
	}
}

func TestAssertJSONKeysReportsExtraKey(t *testing.T) {
	noUpdate(t)
	v := map[string]any{"schema": 1, "name": "x", "added": true}

	f := run(v, filepath.Join("testdata", "flat.golden"))

	msg := f.message()
	if len(f.fatals) != 1 {
		t.Fatalf("fatals = %d, want 1; message:\n%s", len(f.fatals), msg)
	}
	if !strings.Contains(msg, "extra") || !strings.Contains(msg, "+ added") {
		t.Errorf("message does not list the extra key:\n%s", msg)
	}
	if strings.Contains(msg, "missing") {
		t.Errorf("message lists missing keys when none are:\n%s", msg)
	}
}

func TestAssertJSONKeysReportsMissingKey(t *testing.T) {
	noUpdate(t)
	v := map[string]any{"schema": 1}

	f := run(v, filepath.Join("testdata", "flat.golden"))

	msg := f.message()
	if len(f.fatals) != 1 {
		t.Fatalf("fatals = %d, want 1; message:\n%s", len(f.fatals), msg)
	}
	if !strings.Contains(msg, "missing") || !strings.Contains(msg, "- name") {
		t.Errorf("message does not list the missing key:\n%s", msg)
	}
	if strings.Contains(msg, "extra") {
		t.Errorf("message lists extra keys when none are:\n%s", msg)
	}
}

func TestAssertJSONKeysReportsKeyThatBecomesAnObject(t *testing.T) {
	noUpdate(t)
	v := map[string]any{"schema": 1, "name": map[string]any{"first": "x"}}

	f := run(v, filepath.Join("testdata", "flat.golden"))

	msg := f.message()
	if !strings.Contains(msg, "+ name.first") || !strings.Contains(msg, "- name\n") {
		t.Errorf("message does not show the nesting change:\n%s", msg)
	}
}

func TestAssertJSONKeysCollapsesArrays(t *testing.T) {
	noUpdate(t)
	v := map[string]any{
		"rows": []any{
			map[string]any{"a": 1},
			map[string]any{"b": 2, "c": []int{1, 2}},
			"scalar",
		},
		"grid": [][]int{{1}, {2, 3}},
	}

	f := run(v, filepath.Join("testdata", "arrays.golden"))

	if f.failed() {
		t.Fatalf("AssertJSONKeys failed on collapsed arrays:\n%s", f.message())
	}
}

func TestKeyPaths(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{"scalar root", 3, []string{}},
		{"empty object root", map[string]any{}, []string{}},
		{"null value", map[string]any{"a": nil}, []string{"a"}},
		{"empty array", map[string]any{"a": []int{}}, []string{"a[]"}},
		{"empty object", map[string]any{"a": map[string]int{}}, []string{"a"}},
		{"array root", []map[string]int{{"x": 1}}, []string{"[].x"}},
		{
			"merged array elements",
			map[string]any{"items": []map[string]int{{"a": 1}, {"a": 2, "b": 3}}},
			[]string{"items[].a", "items[].b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := keyPaths(tt.v)
			if err != nil {
				t.Fatalf("keyPaths: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("keyPaths = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssertJSONKeysUpdateModeWritesGolden(t *testing.T) {
	t.Setenv(updateEnv, "1")
	path := filepath.Join(t.TempDir(), "nested", "dir", "record.golden")
	v := map[string]any{"schema": 1, "meta": map[string]any{"owner": "x"}, "tags": []string{"t"}}

	f := run(v, path)

	if f.failed() {
		t.Fatalf("AssertJSONKeys failed in update mode:\n%s", f.message())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written golden file: %v", err)
	}
	if want := "meta.owner\nschema\ntags[]\n"; string(got) != want {
		t.Errorf("golden file = %q, want %q", got, want)
	}

	t.Setenv(updateEnv, "")
	if f := run(v, path); f.failed() {
		t.Errorf("written golden file does not match its value:\n%s", f.message())
	}
}

func TestAssertJSONKeysFailsWithoutGolden(t *testing.T) {
	noUpdate(t)
	path := filepath.Join(t.TempDir(), "absent.golden")

	f := run(map[string]any{"schema": 1}, path)

	msg := f.message()
	if len(f.fatals) != 1 {
		t.Fatalf("fatals = %d, want 1; message:\n%s", len(f.fatals), msg)
	}
	if !strings.Contains(msg, updateEnv+"=1") {
		t.Errorf("message does not say how to create the golden file:\n%s", msg)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("AssertJSONKeys created the golden file outside update mode")
	}
}

func TestAssertJSONKeysFailsOnMarshalError(t *testing.T) {
	noUpdate(t)

	f := run(map[string]any{"ch": make(chan int)}, filepath.Join("testdata", "flat.golden"))

	if len(f.fatals) != 1 || !strings.Contains(f.fatals[0], "marshal") {
		t.Errorf("fatals = %q, want one marshal failure", f.fatals)
	}
}
