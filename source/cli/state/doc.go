// Package state replaces files so that a reader sees either the old content or the new, never a
// partial write, and so that the new content survives a crash once a call returns.
//
// A file is written to a temporary file in the destination's directory, given its final mode,
// synced, renamed over the destination, and the directory is synced so the rename itself is
// durable. Rename is atomic only within one filesystem, which is why the temporary file lives
// beside the destination.
//
// The package takes paths and nothing more: it resolves no directories, takes no lock and creates
// no directory, so writing into a directory that does not exist is an error.
//
// On every failure it reports, the package removes its temporary file. A process killed between
// creating the temporary file and renaming it leaves a file named .<base>.tmp-* beside the
// destination, where <base> is the destination's file name. The destination itself holds the old
// content or the new in full. The leftover file is accepted, and nothing sweeps it.
package state
