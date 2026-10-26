package spec

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.vipengele.org/commons/validate"
)

// CompletionKind is the way a [Completion] proposes values for the word being completed.
type CompletionKind int

// The kinds a [Completion] has. The zero CompletionKind is [NoCompletion].
const (
	// NoCompletion proposes nothing of its own and leaves the shell's default behaviour. It is the
	// kind of the zero [Completion].
	NoCompletion CompletionKind = iota
	// ChoicesCompletion proposes a fixed list of values, as [CompleteChoices] builds, and no file
	// names.
	ChoicesCompletion
	// FilesCompletion proposes file names, limited to some extensions when
	// [Completion.Extensions] names any, as [CompleteFiles] builds.
	FilesCompletion
	// DirsCompletion proposes directory names only, as [CompleteDirs] builds.
	DirsCompletion
	// FuncCompletion proposes the values a [CompletionFunc] returns, as [CompleteFunc] builds,
	// and no file names.
	FuncCompletion
)

// String returns "none", "choices", "files", "dirs" or "func", and "CompletionKind(n)" for any
// other value.
func (k CompletionKind) String() string {
	switch k {
	case NoCompletion:
		return "none"
	case ChoicesCompletion:
		return "choices"
	case FilesCompletion:
		return "files"
	case DirsCompletion:
		return "dirs"
	case FuncCompletion:
		return "func"
	}
	return fmt.Sprintf("CompletionKind(%d)", int(k))
}

// CompletionInput is what a [CompletionFunc] is given about the command line being completed.
type CompletionInput struct {
	// ToComplete is the partial word under the cursor, "" when the cursor starts a new word.
	ToComplete string
	// Args are the positional arguments already on the command line before ToComplete, flags
	// and their values excluded.
	Args []string
}

// CompletionFunc returns the values proposed for in.ToComplete. The shell offers exactly the
// values returned, filtered by the shell to those that start with in.ToComplete where it does
// so, and never file names in their place. A non-nil error offers nothing, and still no file
// names; the error is not shown, since a completing shell has nowhere to show it.
//
// It runs in a process the shell starts for each completion, outside the run of any command:
// its context carries no configuration, logger or [Invocation], and no hook runs around it, so a
// function needs to close over whatever it reads. It writes nothing to the standard streams,
// which carry the completion protocol.
type CompletionFunc func(ctx context.Context, in CompletionInput) ([]string, error)

// Completion describes how a shell completes the value of a flag, attached by [Complete], or a
// command's positional arguments, set as [Command].ArgCompletion. It is built by
// [CompleteChoices], [CompleteFiles], [CompleteDirs] or [CompleteFunc]; the zero value is
// [NoCompletion]. A Completion is immutable, and an adapter maps it to its framework by its
// [Completion.Kind], reading the accessor that kind names.
type Completion struct {
	kind   CompletionKind
	values []string
	fn     CompletionFunc
}

// CompleteChoices proposes exactly values, in order, and no file names.
func CompleteChoices(values ...string) Completion {
	return Completion{kind: ChoicesCompletion, values: slices.Clone(values)}
}

// CompleteFiles proposes file names, and directory names to descend into. With exts it proposes
// only the files whose name ends in one of them; an extension is written with or without its
// leading dot, so "yaml" and ".yaml" are the same extension.
func CompleteFiles(exts ...string) Completion {
	var trimmed []string
	for _, ext := range exts {
		trimmed = append(trimmed, strings.TrimPrefix(ext, "."))
	}
	return Completion{kind: FilesCompletion, values: trimmed}
}

// CompleteDirs proposes directory names only.
func CompleteDirs() Completion {
	return Completion{kind: DirsCompletion}
}

// CompleteFunc proposes the values fn returns for each completion; see [CompletionFunc]. A nil
// fn is reported by [Command.Validate].
func CompleteFunc(fn CompletionFunc) Completion {
	return Completion{kind: FuncCompletion, fn: fn}
}

// Kind returns the way the completion proposes values.
func (c Completion) Kind() CompletionKind { return c.kind }

// Choices returns the values a [ChoicesCompletion] proposes, and nil for any other kind.
func (c Completion) Choices() []string {
	if c.kind != ChoicesCompletion {
		return nil
	}
	return slices.Clone(c.values)
}

// Extensions returns the extensions, without their leading dot, a [FilesCompletion] limits file
// names to, and nil for any file name or any other kind.
func (c Completion) Extensions() []string {
	if c.kind != FilesCompletion {
		return nil
	}
	return slices.Clone(c.values)
}

// Func returns the function of a [FuncCompletion], and nil for any other kind.
func (c Completion) Func() CompletionFunc {
	if c.kind != FuncCompletion {
		return nil
	}
	return c.fn
}

// problem reports a [FuncCompletion] with a nil function.
func (c Completion) problem() error {
	if c.kind == FuncCompletion && c.fn == nil {
		return fmt.Errorf("%w: completion function is nil", validate.ErrInvalid)
	}
	return nil
}
