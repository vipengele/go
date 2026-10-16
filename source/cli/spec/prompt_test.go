package spec_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"go.vipengele.org/cli/spec"
)

// promptEnv returns an Env whose standard input is stdin and is a terminal exactly when
// interactive is true, with its output streams captured.
func promptEnv(stdin io.Reader, interactive bool) (env *spec.Env, stdout, stderr *bytes.Buffer) {
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	env = &spec.Env{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Terminal: func(s spec.Stream) bool {
			return interactive && s == spec.Stdin
		},
	}
	return env, stdout, stderr
}

func promptInvocation(stdin io.Reader, interactive bool) (inv *spec.Invocation, stdout, stderr *bytes.Buffer) {
	env, stdout, stderr := promptEnv(stdin, interactive)
	return spec.NewInvocation(env, nil, nil), stdout, stderr
}

func TestConfirmInteractive(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		opts   spec.ConfirmOptions
		want   bool
		prompt string
	}{
		{"y", "y\n", spec.ConfirmOptions{}, true, "Proceed? [y/n] "},
		{"Y", "Y\n", spec.ConfirmOptions{}, true, "Proceed? [y/n] "},
		{"yes", "yes\n", spec.ConfirmOptions{}, true, "Proceed? [y/n] "},
		{"YeS", "YeS\n", spec.ConfirmOptions{}, true, "Proceed? [y/n] "},
		{"n", "n\n", spec.ConfirmOptions{}, false, "Proceed? [y/n] "},
		{"N", "N\n", spec.ConfirmOptions{}, false, "Proceed? [y/n] "},
		{"no", "no\n", spec.ConfirmOptions{}, false, "Proceed? [y/n] "},
		{"NO", "NO\n", spec.ConfirmOptions{}, false, "Proceed? [y/n] "},
		{"surrounding white space and CRLF", " \tyes \r\n", spec.ConfirmOptions{}, true, "Proceed? [y/n] "},
		{"CRLF no", "n\r\n", spec.ConfirmOptions{}, false, "Proceed? [y/n] "},
		{"yes over a default of no", "y\n", spec.ConfirmOptions{Default: spec.DefaultNo}, true, "Proceed? [y/N] "},
		{"no over a default of yes", "n\n", spec.ConfirmOptions{Default: spec.DefaultYes}, false, "Proceed? [Y/n] "},
		{"Enter gives yes", "\n", spec.ConfirmOptions{Default: spec.DefaultYes}, true, "Proceed? [Y/n] "},
		{"Enter gives no", "\n", spec.ConfirmOptions{Default: spec.DefaultNo}, false, "Proceed? [y/N] "},
		{"white space alone gives the default", "  \r\n", spec.ConfirmOptions{Default: spec.DefaultYes}, true, "Proceed? [Y/n] "},
		{"required takes the default on Enter", "\n",
			spec.ConfirmOptions{Default: spec.DefaultYes, Required: true, Flag: "--yes"}, true, "Proceed? [Y/n] "},
		{"required takes the no default on Enter", "\n",
			spec.ConfirmOptions{Default: spec.DefaultNo, Required: true}, false, "Proceed? [y/N] "},
		{"required with no default", "y\n", spec.ConfirmOptions{Required: true}, true, "Proceed? [y/n] "},
		{"end of input after bytes is the answer", "y", spec.ConfirmOptions{Default: spec.DefaultNo}, true, "Proceed? [y/N] "},
		{"end of input after no", "no", spec.ConfirmOptions{Default: spec.DefaultYes}, false, "Proceed? [Y/n] "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, stdout, stderr := promptInvocation(strings.NewReader(tt.input), true)
			got, err := inv.Confirm("Proceed?", tt.opts)
			if err != nil {
				t.Fatalf("Confirm returned %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("Confirm = %t, want %t", got, tt.want)
			}
			if stderr.String() != tt.prompt {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.prompt)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestConfirmRejectsAnAnswerOutsideTheAcceptedWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		opts  spec.ConfirmOptions
		want  string
	}{
		{"another word", "maybe\n", spec.ConfirmOptions{Default: spec.DefaultYes},
			`"maybe" is not an answer to "Proceed?": answer y, yes, n or no`},
		{"a prefix of yes", "ye\n", spec.ConfirmOptions{},
			`"ye" is not an answer to "Proceed?": answer y, yes, n or no`},
		{"a word with yes in it", "yess\n", spec.ConfirmOptions{},
			`"yess" is not an answer to "Proceed?": answer y, yes, n or no`},
		{"Enter with no default", "\n", spec.ConfirmOptions{},
			`"" is not an answer to "Proceed?": answer y, yes, n or no`},
		{"Enter with an unknown default", "\n", spec.ConfirmOptions{Default: spec.ConfirmDefault(7)},
			`"" is not an answer to "Proceed?": answer y, yes, n or no`},
		{"Enter with required and no default", "\n", spec.ConfirmOptions{Required: true, Flag: "--yes"},
			`"" is not an answer to "Proceed?": answer y, yes, n or no`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, _, stderr := promptInvocation(strings.NewReader(tt.input), true)
			got, err := inv.Confirm("Proceed?", tt.opts)
			if err == nil {
				t.Fatalf("Confirm = %t, nil; want an error", got)
			}
			if got {
				t.Errorf("Confirm = true alongside an error, want false")
			}
			if err.Error() != tt.want {
				t.Errorf("error = %q, want %q", err.Error(), tt.want)
			}
			var usage *spec.UsageError
			if errors.As(err, &usage) || errors.Is(err, spec.ErrAborted) {
				t.Errorf("error = %#v, want neither a UsageError nor ErrAborted", err)
			}
			if strings.Count(stderr.String(), "Proceed?") != 1 {
				t.Errorf("stderr = %q, want the question asked once", stderr.String())
			}
		})
	}
}

func TestConfirmHintOfAnUnknownDefault(t *testing.T) {
	inv, _, stderr := promptInvocation(strings.NewReader("y\n"), true)
	if _, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.ConfirmDefault(7)}); err != nil {
		t.Fatalf("Confirm returned %v, want nil", err)
	}
	if want := "Proceed? [y/n] "; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestPromptInteractive(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		opts   spec.PromptOptions
		want   string
		prompt string
	}{
		{"an answer", "bob\n", spec.PromptOptions{}, "bob", "Name? "},
		{"an answer over the default", "bob\n", spec.PromptOptions{Default: "alice"}, "bob", "Name? [alice] "},
		{"case kept", "BoB\n", spec.PromptOptions{}, "BoB", "Name? "},
		{"inner white space kept", "bob smith\n", spec.PromptOptions{}, "bob smith", "Name? "},
		{"surrounding white space and CRLF trimmed", "  bob \t\r\n", spec.PromptOptions{}, "bob", "Name? "},
		{"Enter gives the default", "\n", spec.PromptOptions{Default: "alice"}, "alice", "Name? [alice] "},
		{"CRLF alone gives the default", "\r\n", spec.PromptOptions{Default: "alice"}, "alice", "Name? [alice] "},
		{"required takes the default on Enter", "\n",
			spec.PromptOptions{Default: "alice", Required: true, Flag: "--name"}, "alice", "Name? [alice] "},
		{"required with no default", "bob\n", spec.PromptOptions{Required: true}, "bob", "Name? "},
		{"end of input after bytes is the answer", "bob", spec.PromptOptions{Default: "alice"}, "bob", "Name? [alice] "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, stdout, stderr := promptInvocation(strings.NewReader(tt.input), true)
			got, err := inv.Prompt("Name?", tt.opts)
			if err != nil {
				t.Fatalf("Prompt returned %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("Prompt = %q, want %q", got, tt.want)
			}
			if stderr.String() != tt.prompt {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.prompt)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestPromptRejectsAnEmptyAnswerWithNoDefault(t *testing.T) {
	for _, input := range []string{"\n", "   \r\n"} {
		inv, _, _ := promptInvocation(strings.NewReader(input), true)
		got, err := inv.Prompt("Name?", spec.PromptOptions{Required: true, Flag: "--name"})
		if err == nil {
			t.Fatalf("Prompt(%q) = %q, nil; want an error", input, got)
		}
		if got != "" {
			t.Errorf("Prompt(%q) = %q alongside an error, want empty", input, got)
		}
		if want := `no answer to "Name?": type a value and press Enter`; err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
		var usage *spec.UsageError
		if errors.As(err, &usage) || errors.Is(err, spec.ErrAborted) {
			t.Errorf("error = %#v, want neither a UsageError nor ErrAborted", err)
		}
	}
}

func TestConfirmNotInteractive(t *testing.T) {
	tests := []struct {
		name string
		opts spec.ConfirmOptions
		// usage is the UsageError text; "" when Confirm answers want.
		usage string
		want  bool
	}{
		{"default yes", spec.ConfirmOptions{Default: spec.DefaultYes, Flag: "--yes"}, "", true},
		{"default no", spec.ConfirmOptions{Default: spec.DefaultNo, Flag: "--yes"}, "", false},
		{"no default", spec.ConfirmOptions{Flag: "--yes"},
			"Proceed? requires interactive input; pass --yes", false},
		{"an unknown default", spec.ConfirmOptions{Default: spec.ConfirmDefault(7), Flag: "--yes"},
			"Proceed? requires interactive input; pass --yes", false},
		{"required with default yes", spec.ConfirmOptions{Default: spec.DefaultYes, Required: true, Flag: "--yes"},
			"Proceed? requires interactive input; pass --yes", false},
		{"required with default no", spec.ConfirmOptions{Default: spec.DefaultNo, Required: true, Flag: "--force"},
			"Proceed? requires interactive input; pass --force", false},
		{"required with no default", spec.ConfirmOptions{Required: true, Flag: "--yes"},
			"Proceed? requires interactive input; pass --yes", false},
		{"no flag", spec.ConfirmOptions{}, "Proceed? requires interactive input", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := bytes.NewBufferString("y\n")
			inv, stdout, stderr := promptInvocation(stdin, false)
			got, err := inv.Confirm("Proceed?", tt.opts)
			if got != tt.want {
				t.Errorf("Confirm = %t, want %t", got, tt.want)
			}
			checkUsageError(t, err, tt.usage, "Proceed?", tt.opts.Flag)
			if stdin.String() != "y\n" {
				t.Errorf("stdin left %q, want it unread", stdin.String())
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
			}
		})
	}
}

func TestPromptNotInteractive(t *testing.T) {
	tests := []struct {
		name  string
		opts  spec.PromptOptions
		usage string
		want  string
	}{
		{"default", spec.PromptOptions{Default: "alice", Flag: "--name"}, "", "alice"},
		{"no default", spec.PromptOptions{Flag: "--name NAME"},
			"Name? requires interactive input; pass --name NAME", ""},
		{"required with a default", spec.PromptOptions{Default: "alice", Required: true, Flag: "--name"},
			"Name? requires interactive input; pass --name", ""},
		{"required with no default", spec.PromptOptions{Required: true, Flag: "--name"},
			"Name? requires interactive input; pass --name", ""},
		{"no flag", spec.PromptOptions{}, "Name? requires interactive input", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := bytes.NewBufferString("bob\n")
			inv, stdout, stderr := promptInvocation(stdin, false)
			got, err := inv.Prompt("Name?", tt.opts)
			if got != tt.want {
				t.Errorf("Prompt = %q, want %q", got, tt.want)
			}
			checkUsageError(t, err, tt.usage, "Name?", tt.opts.Flag)
			if stdin.String() != "bob\n" {
				t.Errorf("stdin left %q, want it unread", stdin.String())
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
			}
		})
	}
}

// checkUsageError checks that err is nil when want is "", and otherwise a bare *spec.UsageError
// with the given text, question and flag.
func checkUsageError(t *testing.T, err error, want, question, flag string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Errorf("error = %v, want nil", err)
		}
		return
	}
	// A type assertion rather than errors.As: the App renders a UsageError as one line only when it
	// is returned bare.
	usage, ok := err.(*spec.UsageError)
	if !ok {
		t.Fatalf("error = %#v, want a bare *spec.UsageError", err)
	}
	if usage.Question != question || usage.Flag != flag {
		t.Errorf("UsageError = %+v, want Question %q and Flag %q", usage, question, flag)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestPromptsAreInteractiveOnlyWhenStdinIsATerminal(t *testing.T) {
	// Every stream but the standard input is a terminal.
	var queried []spec.Stream
	stdin := bytes.NewBufferString("y\n")
	env := &spec.Env{
		Stdin: stdin,
		Terminal: func(s spec.Stream) bool {
			queried = append(queried, s)
			return s != spec.Stdin
		},
	}
	inv := spec.NewInvocation(env, nil, nil)
	if _, err := inv.Confirm("Proceed?", spec.ConfirmOptions{}); err == nil {
		t.Errorf("Confirm returned nil, want a UsageError")
	}
	if _, err := inv.Prompt("Name?", spec.PromptOptions{}); err == nil {
		t.Errorf("Prompt returned nil, want a UsageError")
	}
	for _, s := range queried {
		if s != spec.Stdin {
			t.Errorf("Terminal queried for %v, want stdin only", s)
		}
	}
	if stdin.String() != "y\n" {
		t.Errorf("stdin left %q, want it unread", stdin.String())
	}
}

func TestPromptsOnAnInvocationWithNoEnv(t *testing.T) {
	inv := &spec.Invocation{}
	if _, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Flag: "--yes"}); err == nil {
		t.Errorf("Confirm returned nil, want a UsageError")
	}
	got, err := inv.Prompt("Name?", spec.PromptOptions{Default: "alice"})
	if err != nil || got != "alice" {
		t.Errorf("Prompt = %q, %v; want %q, nil", got, err, "alice")
	}
	yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.DefaultYes})
	if err != nil || !yes {
		t.Errorf("Confirm = %t, %v; want true, nil", yes, err)
	}
}

func TestEndOfInputBeforeAnyByteAborts(t *testing.T) {
	hints := map[spec.ConfirmDefault]string{spec.NoDefault: "[y/n] ", spec.DefaultYes: "[Y/n] ", spec.DefaultNo: "[y/N] "}
	confirms := []spec.ConfirmOptions{{}, {Default: spec.DefaultYes}, {Default: spec.DefaultNo}, {Required: true}}
	for _, opts := range confirms {
		inv, stdout, stderr := promptInvocation(strings.NewReader(""), true)
		got, err := inv.Confirm("Proceed?", opts)
		if !errors.Is(err, spec.ErrAborted) {
			t.Errorf("Confirm(%+v) error = %v, want ErrAborted", opts, err)
		}
		if got {
			t.Errorf("Confirm(%+v) = true, want false and never the default", opts)
		}
		if stdout.Len() != 0 || stderr.String() != "Proceed? "+hints[opts.Default] {
			t.Errorf("Confirm(%+v) wrote stdout %q, stderr %q; want the question alone", opts, stdout.String(), stderr.String())
		}
	}
	prompts := []spec.PromptOptions{{}, {Default: "alice"}, {Default: "alice", Required: true}}
	for _, opts := range prompts {
		inv, stdout, stderr := promptInvocation(strings.NewReader(""), true)
		got, err := inv.Prompt("Name?", opts)
		if !errors.Is(err, spec.ErrAborted) {
			t.Errorf("Prompt(%+v) error = %v, want ErrAborted", opts, err)
		}
		if got != "" {
			t.Errorf("Prompt(%+v) = %q, want empty and never the default", opts, got)
		}
		wantStderr := "Name? "
		if opts.Default != "" {
			wantStderr += "[" + opts.Default + "] "
		}
		if stdout.Len() != 0 || stderr.String() != wantStderr {
			t.Errorf("Prompt(%+v) wrote stdout %q, stderr %q; want the question alone", opts, stdout.String(), stderr.String())
		}
	}
}

func TestErrAbortedText(t *testing.T) {
	if got := spec.ErrAborted.Error(); got != "aborted" {
		t.Errorf("ErrAborted = %q, want %q", got, "aborted")
	}
}

// eofWithByte returns its one byte together with io.EOF, as an io.Reader may.
type eofWithByte struct {
	b    byte
	done bool
}

func (r *eofWithByte) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	p[0] = r.b
	return 1, io.EOF
}

func TestAByteReadWithEndOfInputIsKept(t *testing.T) {
	inv, _, _ := promptInvocation(&eofWithByte{b: 'y'}, true)
	got, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.DefaultNo})
	if err != nil || !got {
		t.Errorf("Confirm = %t, %v; want true, nil", got, err)
	}

	inv, _, _ = promptInvocation(&eofWithByte{b: '\n'}, true)
	got, err = inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.DefaultYes})
	if err != nil || !got {
		t.Errorf("Confirm on a newline with end of input = %t, %v; want the default, true, nil", got, err)
	}
}

// emptyReads returns n empty reads with no error before reading from r.
type emptyReads struct {
	n int
	r io.Reader
}

func (e *emptyReads) Read(p []byte) (int, error) {
	if e.n > 0 {
		e.n--
		return 0, nil
	}
	return e.r.Read(p)
}

func TestAnEmptyReadIsRetried(t *testing.T) {
	inv, _, _ := promptInvocation(&emptyReads{n: 3, r: strings.NewReader("bob\n")}, true)
	got, err := inv.Prompt("Name?", spec.PromptOptions{})
	if err != nil || got != "bob" {
		t.Errorf("Prompt = %q, %v; want %q, nil", got, err, "bob")
	}
}

// failAfter returns data, then fails every read with err.
type failAfter struct {
	data string
	err  error
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestAReadErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	for _, data := range []string{"", "bo"} {
		inv, _, _ := promptInvocation(&failAfter{data: data, err: boom}, true)
		got, err := inv.Prompt("Name?", spec.PromptOptions{Default: "alice"})
		if !errors.Is(err, boom) {
			t.Errorf("Prompt after %q error = %v, want it to wrap %v", data, err, boom)
		}
		if err != nil && err.Error() != "read answer: boom" {
			t.Errorf("error = %q, want %q", err.Error(), "read answer: boom")
		}
		if errors.Is(err, spec.ErrAborted) || got != "" {
			t.Errorf("Prompt after %q = %q, %v; want empty and not ErrAborted", data, got, err)
		}

		inv, _, _ = promptInvocation(&failAfter{data: data, err: boom}, true)
		yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.DefaultYes})
		if !errors.Is(err, boom) || yes {
			t.Errorf("Confirm after %q = %t, %v; want false and an error wrapping %v", data, yes, err, boom)
		}
	}
}

func TestTheAnswerIsReadUpToTheNewlineAndNoFurther(t *testing.T) {
	stdin := bytes.NewBufferString("y\nbob\r\nrest")
	inv, _, stderr := promptInvocation(stdin, true)
	yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{})
	if err != nil || !yes {
		t.Fatalf("Confirm = %t, %v; want true, nil", yes, err)
	}
	if stdin.String() != "bob\r\nrest" {
		t.Fatalf("after Confirm stdin holds %q, want %q", stdin.String(), "bob\r\nrest")
	}
	name, err := inv.Prompt("Name?", spec.PromptOptions{})
	if err != nil || name != "bob" {
		t.Fatalf("Prompt = %q, %v; want %q, nil", name, err, "bob")
	}
	if stdin.String() != "rest" {
		t.Errorf("after Prompt stdin holds %q, want %q", stdin.String(), "rest")
	}
	if want := "Proceed? [y/n] Name? "; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestQuestionsGoToStderrWhateverTheOutput(t *testing.T) {
	modes := []struct {
		name string
		opts spec.OutputOptions
	}{
		{"default", spec.OutputOptions{}},
		{"quiet", spec.OutputOptions{Quiet: true}},
		{"json", spec.OutputOptions{JSON: true}},
		{"json and quiet", spec.OutputOptions{JSON: true, Quiet: true}},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			inv, stdout, stderr := promptInvocation(strings.NewReader("y\nbob\n"), true)
			inv.Output = spec.NewOutput(m.opts)
			yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{Default: spec.DefaultNo})
			if err != nil || !yes {
				t.Errorf("Confirm = %t, %v; want true, nil", yes, err)
			}
			name, err := inv.Prompt("Name?", spec.PromptOptions{Default: "alice"})
			if err != nil || name != "bob" {
				t.Errorf("Prompt = %q, %v; want %q, nil", name, err, "bob")
			}
			if want := "Proceed? [y/N] Name? [alice] "; stderr.String() != want {
				t.Errorf("stderr = %q, want %q", stderr.String(), want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestAFailedQuestionWriteStillReadsTheAnswer(t *testing.T) {
	env, _, _ := promptEnv(strings.NewReader("y\n"), true)
	env.Stderr = failingWriter{err: errors.New("closed")}
	inv := spec.NewInvocation(env, nil, nil)
	yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{})
	if err != nil || !yes {
		t.Errorf("Confirm = %t, %v; want true, nil", yes, err)
	}
}

// The Invocation carries no context, so a pending interrupt is not observed while the question
// waits: the answer typed before Enter is returned, and the interrupt stays for the App, which
// cancels the run's context on it.
func TestAnInterruptIsNotObservedUntilEnter(t *testing.T) {
	interrupts := make(chan struct{}, 1)
	interrupts <- struct{}{}
	env, _, _ := promptEnv(strings.NewReader("y\n"), true)
	env.Interrupts = interrupts
	inv := spec.NewInvocation(env, nil, nil)
	yes, err := inv.Confirm("Proceed?", spec.ConfirmOptions{})
	if err != nil || !yes {
		t.Errorf("Confirm = %t, %v; want true, nil", yes, err)
	}
	if len(interrupts) != 1 {
		t.Errorf("Confirm consumed the pending interrupt; want it left for the App")
	}
}
