// Package errrender prints an error as a tree: a summary line, then one row per layer of its
// wrap chain and one bullet per error of a join, each indented under the row it belongs to.
//
//	tool: validate config
//	  • name: required
//	  • port: out of range
//	    → strconv: invalid syntax
//
// A layer's own text is its message with its wrapped error's message, and the ": " before it,
// cut off the end. A layer whose message does not end that way cannot be split, so it prints
// whole as one row and nothing beneath it is printed.
package errrender

import (
	"fmt"
	"io"
	"strings"

	"go.vipengele.org/cli/core/internal/cobra"
)

// Renderer renders one error of a tree in its own way. It returns the error's row text as the
// first line and, on any further lines, detail rows printed indented under it, and true; or
// false, or an empty string, for an error it does not render.
type Renderer func(error) (string, bool)

// kind is how a row is marked.
type kind int

const (
	// layer is a row of a wrap chain, marked "→".
	layer kind = iota
	// child is one error of a join, marked "•".
	child
	// detail is a line of a [Renderer]'s output after the first, unmarked.
	detail
)

// node is one row of the tree and the rows indented under it.
type node struct {
	text     string
	kind     kind
	children []*node
}

// Render writes err to w as a tree whose summary line is prefixed "<tool>: ", or carries no
// prefix when tool is empty. Each error of the tree is offered to renderers in order; the first
// that renders it prints that error and replaces everything beneath it. A nil err writes
// nothing.
func Render(w io.Writer, tool string, err error, renderers []Renderer) {
	if err == nil {
		return
	}
	root := build(err, renderers)
	var b strings.Builder
	if tool != "" {
		b.WriteString(tool + ": ")
	}
	b.WriteString(root.text + "\n")
	for _, c := range root.children {
		write(&b, c, 1)
	}
	_, _ = io.WriteString(w, b.String())
}

// write prints n at depth, two spaces per level, and its children one level deeper.
func write(b *strings.Builder, n *node, depth int) {
	b.WriteString(strings.Repeat("  ", depth))
	switch n.kind {
	case layer:
		b.WriteString("→ ")
	case child:
		b.WriteString("• ")
	case detail:
	}
	b.WriteString(n.text + "\n")
	for _, c := range n.children {
		write(b, c, depth+1)
	}
}

// build returns err as one row: the text of its outermost layer that has any, its inner layers
// as its children, and the errors of a join it ends in under its last layer. The caller sets the
// row's kind.
func build(err error, renderers []Renderer) *node {
	rows := chain(err, renderers)
	if len(rows) == 0 {
		return &node{text: err.Error()}
	}
	head := rows[0]
	for _, r := range rows[1:] {
		r.kind = layer
		head.children = append(head.children, r)
	}
	return head
}

// chain walks err's wrap chain from the outside in and returns one row per layer with text of
// its own. It stops at an error a renderer takes, at an error it cannot split, and at a join,
// whose errors it hangs under the last row.
func chain(err error, renderers []Renderer) []*node {
	var rows []*node
	for err != nil {
		if n, ok := typed(err, renderers); ok {
			return append(rows, n)
		}
		if _, ok := err.(*cobra.UsageError); ok {
			return appendText(rows, err.Error())
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			inner := u.Unwrap()
			if inner == nil {
				return appendText(rows, err.Error())
			}
			text, ok := ownText(err, inner)
			if !ok {
				return appendText(rows, err.Error())
			}
			rows = appendText(rows, text)
			err = inner
		case interface{ Unwrap() []error }:
			errs := nonNil(u.Unwrap())
			if len(errs) == 0 || err.Error() != joined(errs) {
				return appendText(rows, err.Error())
			}
			if len(errs) == 1 {
				err = errs[0]
				continue
			}
			if len(rows) == 0 {
				rows = append(rows, &node{text: fmt.Sprintf("%d errors", len(errs))})
			}
			last := rows[len(rows)-1]
			for _, e := range errs {
				c := build(e, renderers)
				c.kind = child
				last.children = append(last.children, c)
			}
			return rows
		default:
			return appendText(rows, err.Error())
		}
	}
	return rows
}

// typed offers err to renderers in order and returns the row of the first that renders it.
func typed(err error, renderers []Renderer) (*node, bool) {
	for _, r := range renderers {
		out, ok := r(err)
		if !ok || out == "" {
			continue
		}
		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		n := &node{text: lines[0]}
		for _, l := range lines[1:] {
			n.children = append(n.children, &node{text: l, kind: detail})
		}
		return n, true
	}
	return nil, false
}

// ownText returns the text err adds to inner: "" when err prints exactly as inner, and false
// when err's message does not end in ": " followed by inner's.
func ownText(err, inner error) (string, bool) {
	msg, innerMsg := err.Error(), inner.Error()
	if msg == innerMsg {
		return "", true
	}
	suffix := ": " + innerMsg
	if !strings.HasSuffix(msg, suffix) {
		return "", false
	}
	return strings.TrimSuffix(msg, suffix), true
}

// appendText appends a row of text to rows, unless text is empty.
func appendText(rows []*node, text string) []*node {
	if text == "" {
		return rows
	}
	return append(rows, &node{text: text})
}

// nonNil returns errs without its nil entries.
func nonNil(errs []error) []error {
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}

// joined returns the messages of errs separated by newlines, as errors.Join prints them.
func joined(errs []error) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}
