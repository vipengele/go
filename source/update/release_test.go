package update

import (
	"errors"
	"os"
	"testing"
)

func TestCheckRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"empty", "", "atk has no release version (a development build); install a release to update"},
		{"dev", "dev", "atk dev is a development build; install a release to update"},
		{
			"pseudo-version", "v0.0.0-20240101000000-abcdef123456",
			"atk v0.0.0-20240101000000-abcdef123456 is a development build; install a release to update",
		},
		{"build metadata", "v1.2.3+dirty", "atk v1.2.3+dirty is a development build; install a release to update"},
		{"release", "v1.2.3", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := validConfig(os.TempDir())
			c.Version = tt.version

			err := c.CheckRelease()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("CheckRelease() = %v, want nil", err)
				}

				return
			}

			var dev *DevBuildError
			if !errors.As(err, &dev) {
				t.Fatalf("CheckRelease() = %v, want *DevBuildError", err)
			}

			if dev.Name != "atk" || dev.Version != tt.version {
				t.Errorf("DevBuildError = %+v, want Name atk and Version %q", dev, tt.version)
			}

			if got := err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
