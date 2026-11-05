package updatecmd

import (
	"testing"

	"go.vipengele.org/update"
)

func TestCommandsRunsEveryOptionAndSkipsNil(t *testing.T) {
	t.Parallel()

	var calls int
	var got *settings
	record := func(s *settings) {
		calls++
		got = s
	}

	cmds := Commands(update.Config{}, nil, record, nil)

	if len(cmds) != 1 {
		t.Fatalf("Commands returned %d commands, want 1", len(cmds))
	}
	if calls != 1 {
		t.Errorf("the non-nil Option ran %d times, want 1", calls)
	}
	if got == nil {
		t.Error("the Option ran with nil settings, want the call's settings")
	}
}

func TestReleaseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.3", false},
		{"1.2.3-rc.1", "v1.2.3-rc.1", false},
		{"v1.2.3-rc.1", "v1.2.3-rc.1", false},
		{"1.2", "", true},
		{"v1.2.3+meta", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := releaseVersion(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("releaseVersion(%q) error = %v, want error %t", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("releaseVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
