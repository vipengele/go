package update

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.vipengele.org/commons/validate"
	"go.vipengele.org/update/asset"
)

// selectConfig is a validConfig for linux/amd64, whose default archive and checksums names are
// "atk_linux_amd64.tar.gz" and "checksums.txt".
func selectConfig(t *testing.T) Config {
	t.Helper()

	c := validConfig(t.TempDir())
	c.GOOS = "linux"
	c.GOARCH = "amd64"
	return c
}

// testArchive is the archive asset selectConfig names.
func testArchive() Asset {
	return Asset{Name: "atk_linux_amd64.tar.gz", URL: "https://example.test/archive", Size: 1234}
}

// testChecksums is the checksums asset selectConfig names.
func testChecksums() Asset {
	return Asset{Name: "checksums.txt", URL: "https://example.test/checksums", Size: 56}
}

func TestSelect_Listed(t *testing.T) {
	t.Parallel()

	rel := Release{Tag: "v1.2.3", Assets: []Asset{
		{Name: "atk_darwin_arm64.tar.gz", URL: "https://example.test/other", Size: 1},
		testChecksums(),
		testArchive(),
	}}
	got, err := selectConfig(t).Select(rel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if want := (Selection{Tag: "v1.2.3", Archive: testArchive(), Checksums: testChecksums()}); got != want {
		t.Errorf("Select() = %+v, want %+v", got, want)
	}
}

// assetNotFound returns the names of the *AssetNotFoundError err holds, each checked to carry
// tag.
func assetNotFound(t *testing.T, err error, tag string) []string {
	t.Helper()

	var names []string
	var visit func(error)
	visit = func(e error) {
		if nf, ok := e.(*AssetNotFoundError); ok {
			if nf.Tag != tag {
				t.Errorf("AssetNotFoundError.Tag = %q, want %q", nf.Tag, tag)
			}
			names = append(names, nf.Name)
			return
		}
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range joined.Unwrap() {
				visit(inner)
			}
		}
	}
	visit(err)
	return names
}

func TestSelect_ListedMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		assets []Asset
		want   []string
	}{
		{name: "archive missing", assets: []Asset{testChecksums()}, want: []string{"atk_linux_amd64.tar.gz"}},
		{name: "checksums missing", assets: []Asset{testArchive()}, want: []string{"checksums.txt"}},
		{
			name:   "both missing",
			assets: []Asset{{Name: "unrelated.zip"}},
			want:   []string{"atk_linux_amd64.tar.gz", "checksums.txt"},
		},
		{name: "empty list", assets: []Asset{}, want: []string{"atk_linux_amd64.tar.gz", "checksums.txt"}},
		{
			name:   "names differ only in case",
			assets: []Asset{{Name: "ATK_linux_amd64.tar.gz"}, {Name: "Checksums.txt"}},
			want:   []string{"atk_linux_amd64.tar.gz", "checksums.txt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := selectConfig(t).Select(Release{Tag: "v1.2.3", Assets: tc.assets})
			if err == nil {
				t.Fatalf("Select() = %+v, want an error", got)
			}
			if got != (Selection{}) {
				t.Errorf("Select() = %+v, want the zero Selection", got)
			}
			var nf *AssetNotFoundError
			if !errors.As(err, &nf) {
				t.Fatalf("errors.As did not find *AssetNotFoundError in %v", err)
			}
			if names := assetNotFound(t, err, "v1.2.3"); !reflect.DeepEqual(names, tc.want) {
				t.Errorf("missing assets = %q, want %q", names, tc.want)
			}
			if errors.Is(err, ErrAssetsNotListed) {
				t.Error("a listed release gave ErrAssetsNotListed")
			}
		})
	}
}

func TestSelect_UnlistedThroughGitHubSource(t *testing.T) {
	t.Parallel()

	c := selectConfig(t)
	c.BinaryName = "my tool#"
	c.Source = &GitHubSource{Owner: testOwner, Repo: testRepo, WebURL: "https://web.test/"}

	got, err := c.Select(Release{Tag: "v1.2.3+meta/x"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	want := Selection{
		Tag:     "v1.2.3+meta/x",
		Located: true,
		Archive: Asset{
			Name: "my tool#_linux_amd64.tar.gz",
			URL:  "https://web.test/octo/tool/releases/download/v1.2.3+meta%2Fx/my%20tool%23_linux_amd64.tar.gz",
		},
		Checksums: Asset{
			Name: "checksums.txt",
			URL:  "https://web.test/octo/tool/releases/download/v1.2.3+meta%2Fx/checksums.txt",
		},
	}
	if got != want {
		t.Errorf("Select() = %+v, want %+v", got, want)
	}
}

func TestSelect_ReleaseFromGitHubSource(t *testing.T) {
	t.Parallel()

	for _, m := range kitModes() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()

			k := newKitRelease(t, t.TempDir(), m.opts...)
			rel, err := k.cfg.Source.Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}

			got, err := k.cfg.Select(rel)
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			want := Selection{
				Tag:       k.rel.Tag,
				Located:   !m.listed,
				Archive:   Asset{Name: k.rel.ArchiveName, URL: k.srv.AssetURL(k.rel.ArchiveName)},
				Checksums: Asset{Name: k.rel.ChecksumsName, URL: k.srv.AssetURL(k.rel.ChecksumsName)},
			}
			if m.listed {
				want.Archive.Size = int64(len(k.rel.Archive))
				want.Checksums.Size = int64(len(k.srv.ChecksumsFile()))
			}
			if got != want {
				t.Errorf("Select() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestSelect_UnlistedThroughTheDefaultSource(t *testing.T) {
	t.Parallel()

	c := selectConfig(t)
	c.Source = nil

	got, err := c.Select(Release{Tag: "v1.2.3"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	const base = "https://github.com/pedromvgomes/agentic-toolkit/releases/download/v1.2.3/"
	want := Selection{
		Tag:       "v1.2.3",
		Located:   true,
		Archive:   Asset{Name: "atk_linux_amd64.tar.gz", URL: base + "atk_linux_amd64.tar.gz"},
		Checksums: Asset{Name: "checksums.txt", URL: base + "checksums.txt"},
	}
	if got != want {
		t.Errorf("Select() = %+v, want %+v", got, want)
	}
}

func TestSelect_TagAndLocated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rel         Release
		wantLocated bool
	}{
		{
			name:        "listed",
			rel:         Release{Tag: "v1.2.3", Assets: []Asset{testArchive(), testChecksums()}},
			wantLocated: false,
		},
		{
			name:        "unlisted",
			rel:         Release{Tag: "v4.5.6"},
			wantLocated: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := selectConfig(t)
			c.Source = &GitHubSource{Owner: testOwner, Repo: testRepo, WebURL: "https://web.test/"}
			got, err := c.Select(tt.rel)
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			if got.Tag != tt.rel.Tag {
				t.Errorf("Tag = %q, want %q", got.Tag, tt.rel.Tag)
			}
			if got.Located != tt.wantLocated {
				t.Errorf("Located = %v, want %v", got.Located, tt.wantLocated)
			}
		})
	}
}

func TestSelect_UnlistedWithoutALocator(t *testing.T) {
	t.Parallel()

	got, err := selectConfig(t).Select(Release{Tag: "v1.2.3"})
	if !errors.Is(err, ErrAssetsNotListed) {
		t.Fatalf("Select() error = %v, want ErrAssetsNotListed", err)
	}
	if got != (Selection{}) {
		t.Errorf("Select() = %+v, want the zero Selection", got)
	}
}

func TestSelect_RendersBinaryNameNotIdentityName(t *testing.T) {
	t.Parallel()

	c := selectConfig(t)
	c.BinaryName = "atk-cli"
	archive := Asset{Name: "atk-cli_linux_amd64.tar.gz", URL: "https://example.test/cli"}
	rel := Release{Tag: "v1.2.3", Assets: []Asset{testArchive(), archive, testChecksums()}}

	got, err := c.Select(rel)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.Archive != archive {
		t.Errorf("Archive = %+v, want %+v", got.Archive, archive)
	}
}

func TestSelect_CustomTemplates(t *testing.T) {
	t.Parallel()

	c := selectConfig(t)
	c.GOOS = "windows"
	c.GOARCH = "arm64"
	c.AssetTemplate = "{name}-{version}-{os}-{arch}.{ext}"
	c.AssetExt = "zip"
	c.ChecksumsAsset = "{name}_{tag}_SHA256SUMS"
	archive := Asset{Name: "atk-1.2.3-windows-arm64.zip", URL: "https://example.test/zip", Size: 9}
	checksums := Asset{Name: "atk_v1.2.3_SHA256SUMS", URL: "https://example.test/sums", Size: 3}

	got, err := c.Select(Release{Tag: "v1.2.3", Assets: []Asset{checksums, archive}})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if want := (Selection{Tag: "v1.2.3", Archive: archive, Checksums: checksums}); got != want {
		t.Errorf("Select() = %+v, want %+v", got, want)
	}
}

func TestSelect_FailsWhenTheConfigDoesNotResolve(t *testing.T) {
	t.Parallel()

	invalid := selectConfig(t)
	invalid.Name = ""
	if _, err := invalid.Select(Release{Tag: "v1.2.3", Assets: []Asset{testArchive(), testChecksums()}}); !errors.Is(err, validate.ErrEmpty) {
		t.Errorf("Select() with an empty Name error = %v, want validate.ErrEmpty", err)
	}

	homeErr := errors.New("no home")
	noHome := selectConfig(t)
	noHome.UserHomeDir = func() (string, error) { return "", homeErr }
	if _, err := noHome.Select(Release{Tag: "v1.2.3", Assets: []Asset{testArchive(), testChecksums()}}); !errors.Is(err, homeErr) {
		t.Errorf("Select() without a home directory error = %v, want %v", err, homeErr)
	}
}

func TestSelectAssets_TemplateThatDoesNotRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "archive template", cfg: Config{AssetTemplate: "{unknown}", ChecksumsAsset: "checksums.txt", BinaryName: "atk"}},
		{name: "checksums template", cfg: Config{AssetTemplate: "atk.tar.gz", ChecksumsAsset: "{unknown}", BinaryName: "atk"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.cfg.selectAssets(Release{Tag: "v1.2.3", Assets: []Asset{}})
			if !errors.Is(err, asset.ErrUnknownPlaceholder) {
				t.Errorf("selectAssets() error = %v, want %v", err, asset.ErrUnknownPlaceholder)
			}
			if got != (Selection{}) {
				t.Errorf("selectAssets() = %+v, want the zero Selection", got)
			}
		})
	}
}
