package installscript

import (
	"strings"
	"testing"
)

func TestExecuteReportsParseError(t *testing.T) {
	t.Parallel()
	_, err := execute("{{.Name", scriptData{})
	if err == nil || !strings.Contains(err.Error(), "parsing the script template") {
		t.Fatalf("execute with a malformed template error = %v, want the parse error", err)
	}
}

func TestExecuteReportsExecutionError(t *testing.T) {
	t.Parallel()
	_, err := execute("{{.Missing}}", scriptData{})
	if err == nil || !strings.Contains(err.Error(), "rendering the script") {
		t.Fatalf("execute with an unknown field error = %v, want the execution error", err)
	}
}

func TestExecuteRendersData(t *testing.T) {
	t.Parallel()
	got, err := execute("name={{.Name}}", scriptData{Name: "'atk'"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if string(got) != "name='atk'" {
		t.Errorf("execute = %q, want %q", got, "name='atk'")
	}
}
