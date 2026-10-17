package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/version"
	"golang.org/x/mod/semver"
)

// defaultGitHubWebURL is the [GitHubSource.WebURL] a GitHubSource without one uses.
const defaultGitHubWebURL = "https://github.com"

// defaultGitHubAPIURL is the [GitHubSource.APIURL] a GitHubSource without one uses.
const defaultGitHubAPIURL = "https://api.github.com"

// defaultGitHubTimeout is the [GitHubSource.Timeout] a GitHubSource without one uses.
const defaultGitHubTimeout = 10 * time.Second

// gitHubUserAgent is the User-Agent of every request a GitHubSource makes. GitHub rejects REST
// API requests that carry none.
const gitHubUserAgent = "go.vipengele.org/update"

// gitHubAccept is the Accept header of every request a GitHubSource makes.
const gitHubAccept = "application/vnd.github+json"

// gitHubAPIVersion is the X-GitHub-Api-Version header of every request a GitHubSource makes: the
// REST API version the response shapes below are read against.
const gitHubAPIVersion = "2022-11-28"

// maxResponseBody is the most bytes a GitHubSource reads from a successful response. A longer
// body is an error rather than a truncated document.
const maxResponseBody = 8 << 20

// maxDrainBody is the most bytes a GitHubSource discards from a response it has finished with,
// so that a short unread remainder does not cost the connection while a long one is not read
// in full.
const maxDrainBody = 64 << 10

// GitHubSource is a [Source] that reads the latest release of a GitHub repository. Its zero
// value, given Owner and Repo, talks to github.com.
//
// Latest first requests the web page WebURL/<Owner>/<Repo>/releases/latest without following
// its redirect: a redirect status (301, 302, 303, 307 or 308) whose Location ends in
// /<Owner>/<Repo>/releases/tag/<tag> names the latest release, and Latest returns its tag and
// page with nil Assets, meaning the assets are not listed. Any other response, and a failed
// request, falls back to the REST API, APIURL/repos/<Owner>/<Repo>/releases/latest, whose answer
// carries every asset. A timeout or a cancellation of the redirect request does not fall back.
//
// The tag must be a release version as [version.IsRelease] defines it, or Latest returns a
// [*TagError]; a prefixed tag such as "source/update/v1.2.3" is one. A tag with a pre-release
// suffix, such as "v1.2.3-rc.1", gives [ErrPrerelease] on either path. A response with a
// non-success status gives a [*StatusError], an expired deadline a [*TimeoutError], and a
// cancelled ctx an error matching [context.Canceled].
type GitHubSource struct {
	// Owner is the GitHub user or organization that owns the repository.
	Owner string
	// Repo is the name of the repository.
	Repo string
	// HTTPClient makes every request. Nil means a new [http.Client] for each call to Latest,
	// never [http.DefaultClient]. The redirect request uses a copy of it that does not follow
	// redirects.
	HTTPClient *http.Client
	// WebURL is the base address of GitHub's web pages. Empty means "https://github.com".
	WebURL string
	// APIURL is the base address of GitHub's REST API. Empty means "https://api.github.com".
	APIURL string
	// IncludePrereleases asks Latest to consider pre-releases. Latest reports an error when it
	// is set: only the latest stable release can be looked up.
	IncludePrereleases bool
	// Timeout bounds each request Latest makes, through its context; a shorter deadline on the
	// caller's context wins. Zero or negative means 10 seconds.
	Timeout time.Duration
}

// NewGitHubSource returns a GitHubSource for the repository identity names, Owner/Repo, making
// its requests through httpClient. A nil httpClient takes the GitHubSource default.
func NewGitHubSource(identity spec.Identity, httpClient *http.Client) *GitHubSource {
	return &GitHubSource{
		Owner:      identity.Owner,
		Repo:       identity.Repo,
		HTTPClient: httpClient,
	}
}

// Latest returns the repository's latest release. ctx bounds the whole lookup and each request is
// further bounded by Timeout.
func (s *GitHubSource) Latest(ctx context.Context) (Release, error) {
	if s.IncludePrereleases {
		return Release{}, errors.New("update: GitHubSource.IncludePrereleases is not supported")
	}

	return s.latestStable(ctx)
}

// latestStable returns the latest stable release: by the web redirect when it names one, and by
// the REST API otherwise.
func (s *GitHubSource) latestStable(ctx context.Context) (Release, error) {
	rel, found, err := s.latestByRedirect(ctx)
	if err != nil || found {
		return rel, err
	}

	return s.latestByAPI(ctx)
}

// latestByRedirect requests the latest-release web page without following its redirect. found
// is false, with a nil error, when the response is not a redirect to a release tag page, and when
// the request fails for any reason other than a timeout or a cancellation: [http.Client] itself
// fails a redirect whose Location does not parse, before CheckRedirect is consulted.
func (s *GitHubSource) latestByRedirect(ctx context.Context) (rel Release, found bool, err error) {
	pageURL := strings.TrimSuffix(s.webURL(), "/") + "/" + s.repoPath() + "/releases/latest"

	client := *s.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	answered := false
	err = s.get(ctx, &client, pageURL, func(resp *http.Response) error {
		answered = true
		if !isRedirect(resp.StatusCode) {
			return nil
		}

		// A redirect without a usable Location is a miss, not a failure.
		loc, locErr := resp.Location()
		if locErr != nil {
			return nil
		}

		tag, ok := tagFromLocation(resp.Request.URL, loc)
		if !ok {
			return nil
		}

		if err := checkStableTag(tag); err != nil {
			return err
		}

		rel = Release{Tag: tag, URL: loc.String()}
		found = true

		return nil
	})
	if err != nil && !answered && !isTimeoutOrCanceled(err) {
		return Release{}, false, nil
	}

	return rel, found, err
}

// isTimeoutOrCanceled reports whether err, as [classifyError] returns it, is a [*TimeoutError]
// or a cancellation.
func isTimeoutOrCanceled(err error) bool {
	var timeout *TimeoutError

	return errors.As(err, &timeout) || errors.Is(err, context.Canceled)
}

// gitHubRelease is the part of a REST API release object a GitHubSource reads.
type gitHubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []gitHubAsset `json:"assets"`
}

// gitHubAsset is the part of a REST API release asset object a GitHubSource reads.
type gitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// latestByAPI reads the latest release from the REST API. Its Assets are never nil: an empty
// slice is a release published without assets.
func (s *GitHubSource) latestByAPI(ctx context.Context) (Release, error) {
	apiURL := strings.TrimSuffix(s.apiURL(), "/") + "/repos/" + s.repoPath() + "/releases/latest"

	var rel Release

	err := s.get(ctx, s.client(), apiURL, func(resp *http.Response) error {
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return statusError(apiURL, resp)
		}

		body, err := readLimited(resp.Body)
		if err != nil {
			return fmt.Errorf("update: GET %s: %w", apiURL, err)
		}

		var gr gitHubRelease
		if err := json.Unmarshal(body, &gr); err != nil {
			return fmt.Errorf("update: GET %s: decoding release: %w", apiURL, err)
		}

		if err := checkStableTag(gr.TagName); err != nil {
			return err
		}

		assets := make([]Asset, 0, len(gr.Assets))
		for _, a := range gr.Assets {
			assets = append(assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
		}

		rel = Release{Tag: gr.TagName, URL: gr.HTMLURL, Assets: assets}

		return nil
	})
	if err != nil {
		return Release{}, err
	}

	return rel, nil
}

// get makes one GET request for rawURL through client, bounded by Timeout, and hands the
// response to read. The body is drained and closed after read returns, and every error is
// passed through [classifyError].
func (s *GitHubSource) get(ctx context.Context, client *http.Client, rawURL string, read func(*http.Response) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("update: GET %s: %w", rawURL, err)
	}

	req.Header.Set("User-Agent", gitHubUserAgent)
	req.Header.Set("Accept", gitHubAccept)
	req.Header.Set("X-GitHub-Api-Version", gitHubAPIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return classifyError(rawURL, err)
	}
	defer drainAndClose(resp.Body)

	return classifyError(rawURL, read(resp))
}

// client returns HTTPClient, or a new client when it is nil.
func (s *GitHubSource) client() *http.Client {
	if s.HTTPClient == nil {
		return &http.Client{}
	}

	return s.HTTPClient
}

// webURL returns WebURL, or its default when it is empty.
func (s *GitHubSource) webURL() string {
	if s.WebURL == "" {
		return defaultGitHubWebURL
	}

	return s.WebURL
}

// apiURL returns APIURL, or its default when it is empty.
func (s *GitHubSource) apiURL() string {
	if s.APIURL == "" {
		return defaultGitHubAPIURL
	}

	return s.APIURL
}

// timeout returns Timeout, or its default when it is not positive.
func (s *GitHubSource) timeout() time.Duration {
	if s.Timeout <= 0 {
		return defaultGitHubTimeout
	}

	return s.Timeout
}

// repoPath returns "<Owner>/<Repo>" with each part escaped as a path segment.
func (s *GitHubSource) repoPath() string {
	return url.PathEscape(s.Owner) + "/" + url.PathEscape(s.Repo)
}

// isRedirect reports whether code is a status that redirects to another resource. 304 Not
// Modified carries no new location and is not one.
func isRedirect(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// tagFromLocation returns the tag a redirect from the latest-release page at requested names. The
// location's path must be the requested page's directory followed by /tag/<tag>, compared without
// regard to case, as GitHub matches owner and repository names; the tag is everything after it,
// unescaped. ok is false when the location does not have that form or names an empty tag.
func tagFromLocation(requested, location *url.URL) (tag string, ok bool) {
	prefix := path.Dir(requested.EscapedPath()) + "/tag/"
	escaped := location.EscapedPath()

	if len(escaped) <= len(prefix) || !strings.EqualFold(escaped[:len(prefix)], prefix) {
		return "", false
	}

	tag, err := url.PathUnescape(escaped[len(prefix):])
	if err != nil || tag == "" {
		return "", false
	}

	return tag, true
}

// checkStableTag returns nil for a release version without a pre-release suffix, a [*TagError]
// for a tag that is not a release version, and an error wrapping [ErrPrerelease] for a
// pre-release.
func checkStableTag(tag string) error {
	if !version.IsRelease(tag) {
		return &TagError{Tag: tag}
	}

	if semver.Prerelease(tag) != "" {
		return fmt.Errorf("%w: %s", ErrPrerelease, tag)
	}

	return nil
}

// statusError returns the [*StatusError] for resp, a response to rawURL, keeping the start of
// its body. One byte past the kept length is read so that the cut lands on a character boundary.
func statusError(rawURL string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))

	return newStatusError(rawURL, resp.StatusCode, string(body))
}

// readLimited reads r to its end, failing when it holds more than maxResponseBody bytes.
func readLimited(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if len(body) > maxResponseBody {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBody)
	}

	return body, nil
}

// drainAndClose discards at most maxDrainBody bytes of body and closes it.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBody))
	_ = body.Close()
}
