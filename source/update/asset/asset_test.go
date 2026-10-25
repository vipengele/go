package asset

import (
	"errors"
	"strings"
	"testing"
	"text/template"
)

// goreleaserDefaultPrefix is the version-bearing prefix of GoReleaser's default archive
// name_template, before the optional Arm, Mips and Amd64 suffixes.
const goreleaserDefaultPrefix = "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

// goreleaserData carries the fields a GoReleaser name_template reads.
type goreleaserData struct {
	ProjectName string
	Version     string
	Tag         string
	Os          string
	Arch        string
}

// execute runs a GoReleaser name_template through text/template, as GoReleaser does.
func execute(t *testing.T, nameTemplate string, data goreleaserData) string {
	t.Helper()

	tmpl, err := template.New("name").Parse(nameTemplate)
	if err != nil {
		t.Fatalf("parsing %q: %v", nameTemplate, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("executing %q: %v", nameTemplate, err)
	}
	return b.String()
}

func TestRender(t *testing.T) {
	t.Parallel()

	fields := Fields{Name: "atk", Tag: "v1.2.3", OS: "linux", Arch: "amd64", Ext: "tar.gz"}

	tests := []struct {
		name   string
		tmpl   string
		fields Fields
		want   string
	}{
		{name: "default", tmpl: DefaultTemplate, fields: fields, want: "atk_linux_amd64.tar.gz"},
		{name: "default checksums", tmpl: DefaultChecksumsTemplate, fields: fields, want: "checksums.txt"},
		{name: "name", tmpl: "{name}", fields: fields, want: "atk"},
		{name: "version strips the v", tmpl: "{version}", fields: fields, want: "1.2.3"},
		{
			name:   "version without a v",
			tmpl:   "{version}",
			fields: Fields{Tag: "1.2.3"},
			want:   "1.2.3",
		},
		{
			name:   "version strips one v",
			tmpl:   "{version}",
			fields: Fields{Tag: "vv1"},
			want:   "v1",
		},
		{name: "tag as published", tmpl: "{tag}", fields: fields, want: "v1.2.3"},
		{name: "os", tmpl: "{os}", fields: fields, want: "linux"},
		{name: "arch", tmpl: "{arch}", fields: fields, want: "amd64"},
		{name: "ext", tmpl: "{ext}", fields: fields, want: "tar.gz"},
		{name: "ext mid-template", tmpl: "{name}.{ext}.sig", fields: fields, want: "atk.tar.gz.sig"},
		{
			name:   "every placeholder",
			tmpl:   "{name}-{version}-{tag}-{os}-{arch}.{ext}",
			fields: fields,
			want:   "atk-1.2.3-v1.2.3-linux-amd64.tar.gz",
		},
		{name: "literal only", tmpl: "plain.bin", fields: fields, want: "plain.bin"},
		{name: "lone closing brace is literal", tmpl: "a}b", fields: fields, want: "a}b"},
		{
			name:   "substituted value is not expanded",
			tmpl:   "{name}_{os}",
			fields: Fields{Name: "{os}", OS: "{arch}", Arch: "amd64"},
			want:   "{os}_{arch}",
		},
		{
			name:   "substituted brace is not a placeholder",
			tmpl:   "{tag}",
			fields: Fields{Tag: "{unclosed"},
			want:   "{unclosed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Render(tc.tmpl, tc.fields)
			if err != nil {
				t.Fatalf("Render(%q) error: %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestValidate_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tmpl string
		want error
	}{
		{name: "empty", tmpl: "", want: ErrEmptyTemplate},
		{name: "unknown", tmpl: "{name}_{platform}", want: ErrUnknownPlaceholder},
		{name: "empty placeholder", tmpl: "a{}b", want: ErrUnknownPlaceholder},
		{name: "case matters", tmpl: "{Name}", want: ErrUnknownPlaceholder},
		{name: "doubled brace", tmpl: "{{name}}", want: ErrUnknownPlaceholder},
		{name: "unclosed", tmpl: "{name}_{os", want: ErrUnclosedPlaceholder},
		{name: "lone opening brace", tmpl: "a{", want: ErrUnclosedPlaceholder},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := Validate(tc.tmpl); !errors.Is(err, tc.want) {
				t.Errorf("Validate(%q) = %v, want %v", tc.tmpl, err, tc.want)
			}
			if got, err := Render(tc.tmpl, Fields{}); !errors.Is(err, tc.want) || got != "" {
				t.Errorf("Render(%q) = %q, %v, want \"\", %v", tc.tmpl, got, err, tc.want)
			}
			if got, err := GoReleaserArchive(tc.tmpl, "atk"); !errors.Is(err, tc.want) || got != "" {
				t.Errorf("GoReleaserArchive(%q) = %q, %v, want \"\", %v", tc.tmpl, got, err, tc.want)
			}
			if got, err := GoReleaserChecksum(tc.tmpl, "atk"); !errors.Is(err, tc.want) || got != "" {
				t.Errorf("GoReleaserChecksum(%q) = %q, %v, want \"\", %v", tc.tmpl, got, err, tc.want)
			}
		})
	}
}

func TestValidate_Accepts(t *testing.T) {
	t.Parallel()

	for _, tmpl := range []string{DefaultTemplate, DefaultChecksumsTemplate, "{name}.{ext}.{ext}", "x"} {
		if err := Validate(tmpl); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", tmpl, err)
		}
	}
}

func TestValidateChecksums(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tmpl string
		want error
	}{
		{name: "default", tmpl: DefaultChecksumsTemplate, want: nil},
		{name: "every other placeholder", tmpl: "{name}_{version}_{tag}_{os}_{arch}.txt", want: nil},
		{name: "ext", tmpl: "checksums.{ext}", want: ErrMisplacedExt},
		{name: "empty", tmpl: "", want: ErrEmptyTemplate},
		{name: "unknown", tmpl: "{sha}", want: ErrUnknownPlaceholder},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateChecksums(tc.tmpl); !errors.Is(err, tc.want) {
				t.Errorf("ValidateChecksums(%q) = %v, want %v", tc.tmpl, err, tc.want)
			}
		})
	}
}

func TestGoReleaserArchive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tmpl   string
		binary string
		want   string
	}{
		{
			name:   "default",
			tmpl:   DefaultTemplate,
			binary: "atk",
			want:   "atk_{{ .Os }}_{{ .Arch }}",
		},
		{
			name:   "goreleaser default shape",
			tmpl:   "{name}_{version}_{os}_{arch}.{ext}",
			binary: "atk",
			want:   strings.ReplaceAll(goreleaserDefaultPrefix, "{{ .ProjectName }}", "atk"),
		},
		{
			name:   "tag",
			tmpl:   "{name}-{tag}-{os}-{arch}.{ext}",
			binary: "atk",
			want:   "atk-{{ .Tag }}-{{ .Os }}-{{ .Arch }}",
		},
		{
			name:   "no ext is kept as written",
			tmpl:   "{name}_{os}_{arch}",
			binary: "atk",
			want:   "atk_{{ .Os }}_{{ .Arch }}",
		},
		{
			name:   "only the trailing dot is dropped",
			tmpl:   "{name}..{ext}",
			binary: "atk",
			want:   "atk.",
		},
		{
			name:   "name is inlined, not expanded",
			tmpl:   DefaultTemplate,
			binary: "{os}",
			want:   `{{ "{" }}os}_{{ .Os }}_{{ .Arch }}`,
		},
		{
			name:   "doubled brace in name is escaped",
			tmpl:   DefaultTemplate,
			binary: "a{{b}}",
			want:   `a{{ "{" }}{{ "{" }}b}}_{{ .Os }}_{{ .Arch }}`,
		},
		{
			name:   "brace in name before an action is escaped",
			tmpl:   "{name}{os}.{ext}",
			binary: "a{",
			want:   `a{{ "{" }}{{ .Os }}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := GoReleaserArchive(tc.tmpl, tc.binary)
			if err != nil {
				t.Fatalf("GoReleaserArchive(%q, %q) error: %v", tc.tmpl, tc.binary, err)
			}
			if got != tc.want {
				t.Errorf("GoReleaserArchive(%q, %q) = %q, want %q", tc.tmpl, tc.binary, got, tc.want)
			}

			fields := Fields{Name: tc.binary, Tag: "v1.2.3", OS: "linux", Arch: "arm64", Ext: DefaultExt}
			rendered, err := Render(tc.tmpl, fields)
			if err != nil {
				t.Fatalf("Render(%q) error: %v", tc.tmpl, err)
			}
			published := execute(t, got, goreleaserData{
				ProjectName: "project",
				Version:     "1.2.3",
				Tag:         "v1.2.3",
				Os:          "linux",
				Arch:        "arm64",
			})
			if strings.Contains(tc.tmpl, "{ext}") {
				published += "." + DefaultExt
			}
			if published != rendered {
				t.Errorf("GoReleaser publishes %q, Render looks for %q", published, rendered)
			}
		})
	}
}

func TestGoReleaserArchive_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tmpl string
		want error
	}{
		{name: "ext mid-template", tmpl: "{name}.{ext}.sig", want: ErrMisplacedExt},
		{name: "ext at the start", tmpl: "{ext}_{name}", want: ErrMisplacedExt},
		{name: "ext at the end without a dot", tmpl: "{name}{ext}", want: ErrMisplacedExt},
		{name: "ext after a placeholder", tmpl: "{name}_{os}{ext}", want: ErrMisplacedExt},
		{name: "ext twice", tmpl: "{name}.{ext}.{ext}", want: ErrMisplacedExt},
		{name: "ext alone", tmpl: "{ext}", want: ErrMisplacedExt},
		{name: "nothing but the extension", tmpl: ".{ext}", want: ErrEmptyTemplate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, err := GoReleaserArchive(tc.tmpl, "atk"); !errors.Is(err, tc.want) || got != "" {
				t.Errorf("GoReleaserArchive(%q) = %q, %v, want \"\", %v", tc.tmpl, got, err, tc.want)
			}
		})
	}
}

func TestGoReleaserChecksum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tmpl   string
		binary string
		want   string
	}{
		{name: "default", tmpl: DefaultChecksumsTemplate, binary: "atk", want: "checksums.txt"},
		{
			name:   "goreleaser default shape",
			tmpl:   "{name}_{version}_checksums.txt",
			binary: "atk",
			want:   "atk_{{ .Version }}_checksums.txt",
		},
		{name: "tag", tmpl: "{name}-{tag}.sha256", binary: "atk", want: "atk-{{ .Tag }}.sha256"},
		{
			name:   "brace in name is escaped",
			tmpl:   "{name}{version}.txt",
			binary: "a{{",
			want:   `a{{ "{" }}{{ "{" }}{{ .Version }}.txt`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := GoReleaserChecksum(tc.tmpl, tc.binary)
			if err != nil {
				t.Fatalf("GoReleaserChecksum(%q, %q) error: %v", tc.tmpl, tc.binary, err)
			}
			if got != tc.want {
				t.Errorf("GoReleaserChecksum(%q, %q) = %q, want %q", tc.tmpl, tc.binary, got, tc.want)
			}

			rendered, err := Render(tc.tmpl, Fields{Name: tc.binary, Tag: "v1.2.3"})
			if err != nil {
				t.Fatalf("Render(%q) error: %v", tc.tmpl, err)
			}
			published := execute(t, got, goreleaserData{ProjectName: "project", Version: "1.2.3", Tag: "v1.2.3"})
			if published != rendered {
				t.Errorf("GoReleaser publishes %q, Render looks for %q", published, rendered)
			}
		})
	}
}

func TestGoReleaserChecksum_RejectsExt(t *testing.T) {
	t.Parallel()

	for _, tmpl := range []string{"checksums.{ext}", "{ext}.txt", "{name}_{ext}_checksums.txt"} {
		if got, err := GoReleaserChecksum(tmpl, "atk"); !errors.Is(err, ErrMisplacedExt) || got != "" {
			t.Errorf("GoReleaserChecksum(%q) = %q, %v, want \"\", %v", tmpl, got, err, ErrMisplacedExt)
		}
	}
}

func TestValidate_ReportsTheOffsetOfTheBadPlaceholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{name: "unknown at the start", tmpl: "{x}_{os}", want: "at offset 0"},
		{name: "unknown after literal text", tmpl: "ab{x}", want: "at offset 2"},
		{name: "unknown after a placeholder", tmpl: "{name}ab{x}", want: "at offset 8"},
		{name: "unclosed after literal text", tmpl: "ab{os", want: "at offset 2"},
		{name: "unclosed after a placeholder", tmpl: "{name}_{os", want: "at offset 7"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tc.tmpl)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate(%q) = %v, want it to contain %q", tc.tmpl, err, tc.want)
			}
		})
	}
}
