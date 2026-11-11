package service

import (
	"bytes"
	"encoding/xml"
	"path"
	"slices"
	"strconv"
)

const (
	// plistHeader opens every property list launchd reads: the XML
	// declaration and Apple's plist DOCTYPE.
	plistHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n"
	// launchdThrottleInterval is the job's ThrottleInterval in seconds, the
	// same delay as the systemd unit's RestartSec=.
	launchdThrottleInterval = 5
)

// defaultLaunchdLogs returns the log paths of a launchd job named name when
// WithLaunchdLogs sets none: one file takes both streams. An agent
// (ScopeUser) logs to <home>/Library/Logs/<name>/<name>.log; a daemon
// (ScopeSystem) logs to /Library/Logs/<name>/<name>.log and ignores home.
func defaultLaunchdLogs(scope Scope, name, home string) launchdLogs {
	root := "/"
	if scope == ScopeUser {
		root = home
	}
	p := path.Join(root, "Library", "Logs", name, name+".log")
	return launchdLogs{stdout: p, stderr: p}
}

// withDefaults returns l with each empty path replaced by the same stream's
// path from def.
func (l launchdLogs) withDefaults(def launchdLogs) launchdLogs {
	if l.stdout == "" {
		l.stdout = def.stdout
	}
	if l.stderr == "" {
		l.stderr = def.stderr
	}
	return l
}

// renderPlist returns the launchd property list for cfg, logging to the paths
// in logs, which are both set.
//
// The top-level dict holds, in this order: Label (cfg.Name),
// ProgramArguments (cfg.Binary then cfg.Args), EnvironmentVariables (cfg.Env
// in sorted key order, absent when cfg.Env is empty), WorkingDirectory (absent
// when cfg.WorkingDir is empty), UserName (cfg.User, on a daemon only and
// absent when cfg.User is empty), RunAtLoad, KeepAlive, ThrottleInterval,
// StandardOutPath and StandardErrorPath. cfg.Description has no plist key and
// is not written.
//
// KeepAlive is the plain <true/>, never a SuccessfulExit=false dict: a
// self-update ends the process with exit status 0, and launchd must relaunch
// it to run the new binary.
//
// Every string goes through xml.EscapeText, so no value can close its element
// or open another. The output is indented with tabs as Apple writes property
// lists, is the same for equal inputs and ends with a newline.
func renderPlist(cfg Config, logs launchdLogs) []byte {
	var b bytes.Buffer
	b.WriteString(plistHeader)
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")

	indent := func(depth int) {
		for range depth {
			b.WriteByte('\t')
		}
	}
	element := func(depth int, name, text string) {
		indent(depth)
		b.WriteString("<" + name + ">")
		// EscapeText writes to a bytes.Buffer, which never fails.
		_ = xml.EscapeText(&b, []byte(text))
		b.WriteString("</" + name + ">\n")
	}
	tag := func(depth int, name string) {
		indent(depth)
		b.WriteString("<" + name + ">\n")
	}
	keyString := func(key, value string) {
		element(1, "key", key)
		element(1, "string", value)
	}
	keyTrue := func(key string) {
		element(1, "key", key)
		indent(1)
		b.WriteString("<true/>\n")
	}

	keyString("Label", cfg.Name)

	element(1, "key", "ProgramArguments")
	tag(1, "array")
	element(2, "string", cfg.Binary)
	for _, a := range cfg.Args {
		element(2, "string", a)
	}
	tag(1, "/array")

	if len(cfg.Env) > 0 {
		keys := make([]string, 0, len(cfg.Env))
		for k := range cfg.Env {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		element(1, "key", "EnvironmentVariables")
		tag(1, "dict")
		for _, k := range keys {
			element(2, "key", k)
			element(2, "string", cfg.Env[k])
		}
		tag(1, "/dict")
	}
	if cfg.WorkingDir != "" {
		keyString("WorkingDirectory", cfg.WorkingDir)
	}
	if cfg.Scope != ScopeUser && cfg.User != "" {
		keyString("UserName", cfg.User)
	}
	keyTrue("RunAtLoad")
	keyTrue("KeepAlive")
	element(1, "key", "ThrottleInterval")
	element(1, "integer", strconv.Itoa(launchdThrottleInterval))
	keyString("StandardOutPath", logs.stdout)
	keyString("StandardErrorPath", logs.stderr)

	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}
