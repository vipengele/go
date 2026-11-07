package service

import (
	"bytes"
	"slices"
	"strings"
)

// renderUnit returns the systemd unit file for cfg, sandboxed by h. Every
// value is escaped for the directive it is written into rather than
// rejected:
//
//   - ExecStart: each word double-quoted with '\' and '"' backslash-escaped,
//     '%' doubled for specifier expansion and '$' doubled for variable
//     expansion.
//   - Environment: one quoted "KEY=value" word per key, in sorted key order,
//     with '%' doubled; systemd expands no variables there, so '$' stays.
//   - ReadWritePaths: each path a quoted word, with '%' doubled.
//   - WorkingDirectory: the raw value with '%' doubled and, when it ends in
//     '\', a '/' appended, which names the same directory.
//   - Description: the raw value with '%' doubled and any trailing run of
//     '\' dropped; a Description that is only backslashes writes no line.
//
// systemd joins a line ending in an unescaped '\' with the next one, so a
// raw value ending in '\' would swallow the directive after it; no line of
// the output ends in '\'. Newlines cannot reach a raw value: Validate
// rejects control characters.
//
// A system unit is ordered after network-online.target, runs as cfg.User
// when it is set and is wanted by multi-user.target; a user unit is wanted
// by default.target. When ProtectSystem or ProtectHome is set,
// cfg.WorkingDir stays writable through ReadWritePaths. The output is the
// same for equal inputs and ends with a newline.
func renderUnit(cfg Config, h Hardening) []byte {
	var b bytes.Buffer
	line := func(key, value string) {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
		b.WriteByte('\n')
	}
	system := cfg.Scope != ScopeUser

	b.WriteString("[Unit]\n")
	if d := strings.TrimRight(cfg.Description, `\`); d != "" {
		line("Description", escapeSpecifiers(d))
	}
	if system {
		line("After", "network-online.target")
		line("Wants", "network-online.target")
	}

	b.WriteString("\n[Service]\n")
	line("Type", "simple")
	words := make([]string, 0, 1+len(cfg.Args))
	for _, w := range append([]string{cfg.Binary}, cfg.Args...) {
		words = append(words, strings.ReplaceAll(quoteWord(w), "$", "$$"))
	}
	line("ExecStart", strings.Join(words, " "))
	if cfg.WorkingDir != "" {
		dir := cfg.WorkingDir
		if strings.HasSuffix(dir, `\`) {
			dir += "/"
		}
		line("WorkingDirectory", escapeSpecifiers(dir))
	}
	if system && cfg.User != "" {
		line("User", escapeSpecifiers(cfg.User))
	}
	keys := make([]string, 0, len(cfg.Env))
	for k := range cfg.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		line("Environment", quoteWord(k+"="+cfg.Env[k]))
	}
	line("Restart", "always")
	line("RestartSec", "5")

	yes := func(key string, on bool) {
		if on {
			line(key, "yes")
		}
	}
	yes("NoNewPrivileges", h.NoNewPrivileges)
	yes("PrivateTmp", h.PrivateTmp)
	if h.ProtectSystem != "" {
		line("ProtectSystem", h.ProtectSystem)
	}
	if h.ProtectHome != "" {
		line("ProtectHome", h.ProtectHome)
	}
	yes("ProtectKernelTunables", h.ProtectKernelTunables)
	yes("ProtectControlGroups", h.ProtectControlGroups)
	yes("RestrictSUIDSGID", h.RestrictSUIDSGID)
	paths := slices.Clone(h.ReadWritePaths)
	if (h.ProtectSystem != "" || h.ProtectHome != "") && cfg.WorkingDir != "" &&
		!slices.Contains(paths, cfg.WorkingDir) {
		paths = append(paths, cfg.WorkingDir)
	}
	if len(paths) > 0 {
		quoted := make([]string, len(paths))
		for i, p := range paths {
			quoted[i] = quoteWord(p)
		}
		line("ReadWritePaths", strings.Join(quoted, " "))
	}

	b.WriteString("\n[Install]\n")
	if system {
		line("WantedBy", "multi-user.target")
	} else {
		line("WantedBy", "default.target")
	}
	return b.Bytes()
}

// quoteWord returns s as one double-quoted systemd word: '\' and '"' are
// backslash-escaped and '%' is doubled, so neither quoting nor specifier
// expansion changes the value systemd reads. An empty s is "".
func quoteWord(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	return `"` + r.Replace(s) + `"`
}

// escapeSpecifiers doubles every '%' in s, so systemd's specifier expansion
// yields s unchanged.
func escapeSpecifiers(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}
