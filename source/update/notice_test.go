package update

import "testing"

func TestNoticeStringText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		notice Notice
		want   string
	}{
		{
			name:   "unmanaged",
			notice: Notice{Name: "atk", Current: "v1.0.0", Latest: "v1.2.3"},
			want:   "atk v1.2.3 is available (you're on v1.0.0). Run 'atk update' to install.",
		},
		{
			name:   "managed",
			notice: Notice{Name: "atk", Current: "v1.0.0", Latest: "v1.2.3", Instead: "pull a newer image of the container"},
			want:   "atk v1.2.3 is available (you're on v1.0.0); pull a newer image of the container.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.notice.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
