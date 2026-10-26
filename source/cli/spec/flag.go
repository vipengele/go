package spec

import (
	"fmt"

	"go.vipengele.org/commons/validate"
)

// FlagKind is the type of a flag's value.
type FlagKind int

// The kinds a [Flag] has. The zero FlagKind is no kind.
const (
	// BoolKind is a flag created by [BoolFlag], holding a bool.
	BoolKind FlagKind = iota + 1
	// StringKind is a flag created by [StringFlag], holding a string.
	StringKind
	// IntKind is a flag created by [IntFlag], holding an int.
	IntKind
)

// String returns "bool", "string" or "int", and "FlagKind(n)" for any other value.
func (k FlagKind) String() string {
	switch k {
	case BoolKind:
		return "bool"
	case StringKind:
		return "string"
	case IntKind:
		return "int"
	}
	return fmt.Sprintf("FlagKind(%d)", int(k))
}

// Flag describes a flag a [Command] declares in its Flags. It is implemented only by the
// handles [BoolFlag], [StringFlag] and [IntFlag] return, so an adapter maps every Flag it is
// given by its [FlagKind].
type Flag interface {
	// Name is the long name, used as --name, and the key of the flag's value in an [Invocation].
	Name() string
	// Shorthand is the one-character short name, used as -x, or "" for none.
	Shorthand() string
	// Usage is the one-line description shown in help.
	Usage() string
	// Kind is the type of the flag's value.
	Kind() FlagKind
	// DefaultValue is the value the flag has when an invocation does not set it: a bool, a
	// string or an int, as Kind says.
	DefaultValue() any
	// Inherited reports whether the flag is available to every descendant of the command that
	// declares it, as well as to that command.
	Inherited() bool
	// Completion is how a shell completes the flag's value, as [Complete] attaches it: the zero
	// [Completion] when none is attached.
	Completion() Completion

	problems() []error
}

// FlagValue is the set of types a flag holds.
type FlagValue interface {
	bool | string | int
}

// FlagOf is the handle of a flag holding a T. It is created once, with the command that lists
// it, and holds no per-execution state: [FlagOf.Get] reads the value from an [Invocation].
type FlagOf[T FlagValue] struct {
	name       string
	shorthand  string
	usage      string
	def        T
	inherited  bool
	defaultErr error
	completion Completion
}

// FlagOption configures a flag at construction.
type FlagOption func(*flagConfig)

type flagConfig struct {
	shorthand  string
	inherited  bool
	def        any
	hasDefault bool
	completion Completion
}

// Shorthand gives the flag a one-character short name: an ASCII letter or digit.
func Shorthand(s string) FlagOption {
	return func(c *flagConfig) { c.shorthand = s }
}

// Inherited makes the flag available to every descendant of the command that declares it.
func Inherited() FlagOption {
	return func(c *flagConfig) { c.inherited = true }
}

// Default sets the value the flag has when an invocation does not set it. Without it the
// default is the zero value of the flag's type. A default whose type differs from the flag's is
// ignored by [FlagOf.Get] and reported by [Command.Validate].
func Default[T FlagValue](v T) FlagOption {
	return func(c *flagConfig) {
		c.def = v
		c.hasDefault = true
	}
}

// Complete attaches c to the flag, so a shell completes the flag's value with it. A bool flag
// takes no value to complete, and [Command.Validate] reports one given a completion.
func Complete(c Completion) FlagOption {
	return func(cfg *flagConfig) { cfg.completion = c }
}

// BoolFlag returns the handle of a bool flag, which defaults to false.
func BoolFlag(name, usage string, opts ...FlagOption) *FlagOf[bool] {
	return newFlag[bool](name, usage, opts)
}

// StringFlag returns the handle of a string flag, which defaults to "".
func StringFlag(name, usage string, opts ...FlagOption) *FlagOf[string] {
	return newFlag[string](name, usage, opts)
}

// IntFlag returns the handle of an int flag, which defaults to 0.
func IntFlag(name, usage string, opts ...FlagOption) *FlagOf[int] {
	return newFlag[int](name, usage, opts)
}

func newFlag[T FlagValue](name, usage string, opts []FlagOption) *FlagOf[T] {
	var cfg flagConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	f := &FlagOf[T]{
		name:       name,
		shorthand:  cfg.shorthand,
		usage:      usage,
		inherited:  cfg.inherited,
		completion: cfg.completion,
	}
	if cfg.hasDefault {
		if v, ok := cfg.def.(T); ok {
			f.def = v
		} else {
			f.defaultErr = fmt.Errorf("%w: default of type %T for a %s flag",
				validate.ErrInvalid, cfg.def, f.Kind())
		}
	}
	return f
}

// Get returns the flag's value in inv. It returns the flag's default when inv is nil, holds no
// value for the flag, or holds a value of another type.
func (f *FlagOf[T]) Get(inv *Invocation) T {
	if inv == nil {
		return f.def
	}
	if v, ok := inv.values[f.name].(T); ok {
		return v
	}
	return f.def
}

// IsSet reports whether the command line of inv sets the flag explicitly, as the adapter names
// it with [ExplicitFlags], whatever the value given: --json=false is set, a --json not given is
// not. It returns false when inv is nil or holds no value of the flag's type under its name, as
// for a flag of another kind that shares the name, so IsSet is true only when [FlagOf.Get]
// returns the value the command line gives.
func (f *FlagOf[T]) IsSet(inv *Invocation) bool {
	if inv == nil || !inv.set[f.name] {
		return false
	}
	_, ok := inv.values[f.name].(T)
	return ok
}

// Name returns the flag's long name.
func (f *FlagOf[T]) Name() string { return f.name }

// Shorthand returns the flag's one-character short name, or "".
func (f *FlagOf[T]) Shorthand() string { return f.shorthand }

// Usage returns the flag's one-line description.
func (f *FlagOf[T]) Usage() string { return f.usage }

// Inherited reports whether the flag is available to the descendants of its command.
func (f *FlagOf[T]) Inherited() bool { return f.inherited }

// Completion returns how a shell completes the flag's value, the zero [Completion] for none.
func (f *FlagOf[T]) Completion() Completion { return f.completion }

// Default returns the flag's default value.
func (f *FlagOf[T]) Default() T { return f.def }

// DefaultValue returns the flag's default value as an any holding a T.
func (f *FlagOf[T]) DefaultValue() any { return f.def }

// Kind returns the kind matching T.
func (f *FlagOf[T]) Kind() FlagKind {
	switch any(f.def).(type) {
	case bool:
		return BoolKind
	case string:
		return StringKind
	default:
		return IntKind
	}
}

// problems reports an invalid name, an invalid shorthand, a mistyped default, a completion on a
// bool flag and a completion function that is nil.
func (f *FlagOf[T]) problems() []error {
	var errs []error
	if err := validate.Slug("flag name", f.name); err != nil {
		errs = append(errs, err)
	}
	if f.shorthand != "" && !validShorthand(f.shorthand) {
		errs = append(errs, &validate.FieldError{
			Field: "flag shorthand",
			Value: f.shorthand,
			Err:   fmt.Errorf("%w: must be one ASCII letter or digit", validate.ErrInvalid),
		})
	}
	if f.defaultErr != nil {
		errs = append(errs, fmt.Errorf("flag %q: %w", f.name, f.defaultErr))
	}
	if f.completion.Kind() != NoCompletion && f.Kind() == BoolKind {
		errs = append(errs, fmt.Errorf("flag %q: %w: a bool flag takes no completion",
			f.name, validate.ErrInvalid))
	}
	if err := f.completion.problem(); err != nil {
		errs = append(errs, fmt.Errorf("flag %q: %w", f.name, err))
	}
	return errs
}

func validShorthand(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
