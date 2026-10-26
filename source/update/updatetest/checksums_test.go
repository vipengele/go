package updatetest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go.vipengele.org/update/asset"
	"go.vipengele.org/update/updatetest"
)

// splitLine returns the digest and name of a rendered checksums line, or ok false when the line
// is not 64 lower-case hex digits, two spaces and a non-empty name.
func splitLine(line string) (digest, name string, ok bool) {
	m := regexp.MustCompile(`^([0-9a-f]{64})  (.+)$`).FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func TestDigestIsLowerHexSHA256(t *testing.T) {
	t.Parallel()
	body := []byte("archive bytes")
	sum := sha256.Sum256(body)
	if got, want := updatetest.Digest(body), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("Digest = %q, want %q", got, want)
	}
}

func TestChecksumLineParses(t *testing.T) {
	t.Parallel()
	body := []byte("archive bytes")
	line := updatetest.ChecksumLine("tool_linux_amd64.tar.gz", body)
	digest, name, ok := splitLine(line)
	if !ok {
		t.Fatalf("ChecksumLine = %q, want `<hex>  <name>`", line)
	}
	if digest != updatetest.Digest(body) || name != "tool_linux_amd64.tar.gz" {
		t.Errorf("ChecksumLine = %q, want the digest of body and the name", line)
	}
}

func TestChecksumsListsEveryAssetInNameOrder(t *testing.T) {
	t.Parallel()
	assets := map[string][]byte{
		"tool_windows_amd64.zip":  []byte("zip"),
		"tool_linux_amd64.tar.gz": []byte("tgz"),
		"tool with spaces.tar.gz": []byte("spaced"),
	}
	got := string(updatetest.Checksums(assets))
	if !strings.HasSuffix(got, "\n") || strings.Contains(got, "\r") {
		t.Fatalf("Checksums = %q, want LF-ended lines", got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	wantNames := []string{"tool with spaces.tar.gz", "tool_linux_amd64.tar.gz", "tool_windows_amd64.zip"}
	if len(lines) != len(wantNames) {
		t.Fatalf("Checksums has %d lines, want %d: %q", len(lines), len(wantNames), got)
	}
	for i, line := range lines {
		digest, name, ok := splitLine(line)
		if !ok {
			t.Fatalf("line %d = %q, want `<hex>  <name>`", i, line)
		}
		if name != wantNames[i] {
			t.Errorf("line %d names %q, want %q", i, name, wantNames[i])
		}
		if digest != updatetest.Digest(assets[name]) {
			t.Errorf("line %d digest = %s, want the digest of %q", i, digest, name)
		}
	}
}

func TestChecksumsOfNoAssetsIsEmpty(t *testing.T) {
	t.Parallel()
	if got := updatetest.Checksums(nil); len(got) != 0 {
		t.Errorf("Checksums(nil) = %q, want empty", got)
	}
}

func TestDefaultChecksumsNameIsTheUpdaterDefault(t *testing.T) {
	t.Parallel()
	got, err := asset.Render(asset.DefaultChecksumsTemplate, asset.Fields{Name: "tool", Tag: "v1.2.3", OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if updatetest.DefaultChecksumsName != got {
		t.Errorf("DefaultChecksumsName = %q, want %q", updatetest.DefaultChecksumsName, got)
	}
}

func ExampleChecksums() {
	sums := updatetest.Checksums(map[string][]byte{"tool_linux_amd64.tar.gz": []byte("")})
	fmt.Print(string(sums))
	// Output:
	// e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  tool_linux_amd64.tar.gz
}
