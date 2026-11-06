package httpserver

import "testing"

func TestCleanPath(t *testing.T) {
	tests := map[string]string{
		"":         "/",
		"livez":    "/livez",
		"/":        "/",
		"/a/../b":  "/b",
		"/probe/":  "/probe/",
		"/a//b/":   "/a/b/",
		"a/b":      "/a/b",
		"/livez":   "/livez",
		"/a/..":    "/",
		"/a/../":   "/",
		"//livez":  "/livez",
		"/a/./b/.": "/a/b",
	}
	for in, want := range tests {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsFinal(t *testing.T) {
	tests := map[int]bool{100: false, 101: true, 103: false, 199: false, 200: true, 204: true, 404: true}
	for code, want := range tests {
		if got := isFinal(code); got != want {
			t.Errorf("isFinal(%d) = %t, want %t", code, got, want)
		}
	}
}

func TestSplitHost(t *testing.T) {
	valid := []struct {
		in, host, port string
		bracketed      bool
	}{
		{"localhost", "localhost", "", false},
		{"localhost:8080", "localhost", "8080", false},
		{"[::1]", "::1", "", true},
		{"[::1]:8080", "::1", "8080", true},
		{"127.0.0.1:80", "127.0.0.1", "80", false},
		{"Example.COM:8443", "Example.COM", "8443", false},
	}
	for _, tt := range valid {
		host, port, bracketed, ok := splitHost(tt.in)
		if host != tt.host || port != tt.port || bracketed != tt.bracketed || !ok {
			t.Errorf("splitHost(%q) = %q, %q, %t, %t; want %q, %q, %t, true",
				tt.in, host, port, bracketed, ok, tt.host, tt.port, tt.bracketed)
		}
	}
	for _, in := range []string{"", "[::1", "[::1]x", "::1", "a:b:c", ":80", "host:x", "[]:80"} {
		host, port, bracketed, ok := splitHost(in)
		if host != "" || port != "" || bracketed || ok {
			t.Errorf("splitHost(%q) = %q, %q, %t, %t; want zero values and false", in, host, port, bracketed, ok)
		}
	}
}
