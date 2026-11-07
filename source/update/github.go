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
	"strconv"
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

// maxResponseBody is the most bytes a GitHubSource reads from a successful response for one
// release. A longer body is an error rather than a truncated document.
const maxResponseBody = 8 << 20

// maxListResponseBody is the most bytes a GitHubSource reads from a successful response listing
// releases. A page holds up to listPageSize releases, and each carries its notes, up to 125,000
// characters, and an object of about 2 KB per asset, so a page of releases with long notes passes
// maxResponseBody. A longer body is an error rather than a truncated document.
const maxListResponseBody = 32 << 20

// listPageSize is the per_page of the release list request: the most GitHub returns in one page.
const listPageSize = 100

// maxDrainBody is the most bytes a GitHubSource discards from a response it has finished with,
// so that a short unread remainder does not cost the connection while a long one is not read
// in full.
const maxDrainBody = 64 << 10

// GitHubSource is a [Source] that reads the releases of a GitHub repository. Its zero value, given
// Owner and Repo, talks to github.com.
//
// On the [Stable] channel, Latest first requests the web page WebURL/<Owner>/<Repo>/releases/latest
// without following its redirect: a redirect status (301, 302, 303, 307 or 308) whose Location
// ends in /<Owner>/<Repo>/releases/tag/<tag> names the latest release, and Latest returns its tag
// and page with nil Assets, meaning the assets are not listed. Any other response, and a failed
// request, falls back to the REST API, APIURL/repos/<Owner>/<Repo>/releases/latest, whose answer
// carries every asset. A timeout or a cancellation of the redirect request does not fall back.
// The tag must be a release version as [version.IsRelease] defines it, or Latest returns a
// [*TagError]; a prefixed tag such as "source/update/v1.2.3" is one. A tag with a pre-release
// suffix, such as "v1.2.3-rc.1", gives [ErrPrerelease] on either path.
//
// On the [Beta] and [Edge] channels, Latest makes neither of those requests. It reads the release
// list, APIURL/repos/<Owner>/<Repo>/releases, page by page, 100 releases to a page, newest-created
// first as GitHub orders it. Drafts and releases whose tag is not a release version are never
// eligible; any other release is eligible when the channel receives it, judged by its tag's
// pre-release suffix and GitHub's pre-release flag. Latest stops after the first page holding an
// eligible release, after a page shorter than 100 releases, or after 10 pages, and returns the
// eligible release with the highest tag in version order among the pages read, with every asset.
// No eligible release within those pages is an error.
//
// A channel other than Stable, Beta and Edge is an error, with no request made. A response with a
// non-success status gives a [*StatusError]. From the REST API, a 403 or 429 that reports an
// exhausted rate limit gives a [*RateLimitError] and a 401 to a request that carried credentials a
// [*TokenRejectedError]; both unwrap to the [*StatusError]. An expired deadline gives a
// [*TimeoutError], and a cancelled ctx an error matching [context.Canceled].
//
// REST API requests, every page of the release list included, carry "Authorization: Bearer
// <token>" when Token returns a non-empty token, and only to the APIURL host; a REST API redirect to
// another host fails with a [*CrossHostRedirectError]. The web request never carries a token, and
// asset downloads, made through [Config.HTTPClient], never do either.
//
// With a Cache, a REST API request for a URL the cache holds an entry for carries
// "If-None-Match: <etag>", and a 304 Not Modified answer is decoded from the cached body exactly as
// a 200 body is. Each page of the release list is a URL of its own, with an entry of its own. A 200
// carrying an ETag replaces the entry, ETag and body together, once the body has been read in full
// within the read limit. A rate-limited, 401 or other non-success response returns its error, never
// the cached body, and a 304 for a URL with no entry is a [*StatusError]. A cache that fails to load
// or save is a miss or a skipped write, never an error of Latest, and nothing is written once ctx is
// done. GitHub exempts a 304 from the rate limit only when the request is authenticated, so without
// a token a conditional request saves the transfer but not the quota.
type GitHubSource struct {
	// Owner is the GitHub user or organization that owns the repository.
	Owner string
	// Repo is the name of the repository.
	Repo string
	// HTTPClient makes every request. Nil means a new [http.Client] for each call to Latest,
	// never [http.DefaultClient]. The redirect request uses a copy of it that does not follow
	// redirects, and the REST API requests a copy that adds the token and stays on the APIURL
	// host; HTTPClient itself is never modified.
	HTTPClient *http.Client
	// WebURL is the base address of GitHub's web pages. Empty means "https://github.com".
	WebURL string
	// APIURL is the base address of GitHub's REST API. Empty means "https://api.github.com".
	APIURL string
	// Timeout bounds each request Latest makes, through its context; a shorter deadline on the
	// caller's context wins. Zero or negative means 10 seconds.
	Timeout time.Duration
	// Now returns the current time, from which a Retry-After delay becomes a reset time in a
	// [*RateLimitError]. Nil means [time.Now].
	Now func() time.Time
	// Token returns the token REST API requests authenticate with. It is called at most once per
	// call to Latest, and only when Latest makes a REST API request; its error fails that request,
	// wrapped. An empty token, and a nil Token, mean unauthenticated requests.
	Token func(ctx context.Context) (string, error)
	// Cache keeps the REST API responses Latest makes its requests conditional on. Nil means
	// unconditional requests and nothing kept.
	Cache ResponseCache
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

// Latest returns the repository's highest release ch receives. ctx bounds the whole lookup and
// each request is further bounded by Timeout.
func (s *GitHubSource) Latest(ctx context.Context, ch Channel) (Release, error) {
	switch ch {
	case Stable:
		return s.latestStable(ctx)
	case Beta, Edge:
		return s.highestListed(ctx, ch)
	default:
		return Release{}, fmt.Errorf("update: unknown channel %s", ch)
	}
}

// AssetURL returns the address GitHub serves the asset name of the release tagged tag from,
// WebURL/<Owner>/<Repo>/releases/download/<tag>/<name>, with tag and name each escaped as a path
// segment. It makes no request, so it does not check that the asset exists.
func (s *GitHubSource) AssetURL(tag, name string) string {
	return strings.TrimSuffix(s.webURL(), "/") + "/" + s.repoPath() + "/releases/download/" +
		url.PathEscape(tag) + "/" + url.PathEscape(name)
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
	err = s.get(ctx, &client, pageURL, "", func(resp *http.Response) error {
		answered = true
		if !isRedirect(resp.StatusCode) {
			return nil
		}

		// A redirect without a usable Location is a miss, not a failure.
		loc, locErr := resp.Location()
		if locErr != nil {
			return nil
		}

		tag := tagFromLocation(resp.Request.URL, loc)
		if tag == "" {
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
	TagName    string        `json:"tag_name"`
	HTMLURL    string        `json:"html_url"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []gitHubAsset `json:"assets"`
}

// release returns the Release gr describes. Its Assets are never nil: an empty slice is a release
// published without assets.
func (gr gitHubRelease) release() Release {
	assets := make([]Asset, 0, len(gr.Assets))
	for _, a := range gr.Assets {
		assets = append(assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}

	return Release{Tag: gr.TagName, URL: gr.HTMLURL, Assets: assets}
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

	client, err := s.apiClient(ctx)
	if err != nil {
		return Release{}, err
	}

	var rel Release

	err = s.apiGet(ctx, client, apiURL, maxResponseBody, func(body []byte) error {
		var gr gitHubRelease
		if err := json.Unmarshal(body, &gr); err != nil {
			return fmt.Errorf("update: GET %s: decoding release: %w", apiURL, err)
		}

		if err := checkStableTag(gr.TagName); err != nil {
			return err
		}

		rel = gr.release()

		return nil
	})
	if err != nil {
		return Release{}, err
	}

	return rel, nil
}

// maxListPages is the most pages of the release list a GitHubSource reads for one lookup. A
// repository publishing that many releases the channel does not receive, newer than every one it
// does, has its older releases left unread.
const maxListPages = 10

// errNoListedRelease reports a release list with no release Latest may return within the pages
// read: every entry is a draft, has a tag that is not a release version or is on a channel the
// lookup does not receive, or the list is empty.
var errNoListedRelease = errors.New("no release the channel receives")

// highestListed reads the REST API's release list page by page and returns the release ch
// receives with the highest tag in version order among the pages read. Drafts and releases whose
// tag is not a release version are skipped; order within and across pages does not matter. It
// stops after the first page holding a release ch receives, after a page shorter than
// listPageSize, and after maxListPages pages.
func (s *GitHubSource) highestListed(ctx context.Context, ch Channel) (Release, error) {
	base := fmt.Sprintf("%s/repos/%s/releases?per_page=%d",
		strings.TrimSuffix(s.apiURL(), "/"), s.repoPath(), listPageSize)

	var (
		best  *gitHubRelease
		pages int
	)

	client, err := s.apiClient(ctx)
	if err != nil {
		return Release{}, err
	}

	for pages < maxListPages {
		pages++

		list, err := s.listPage(ctx, client, fmt.Sprintf("%s&page=%d", base, pages))
		if err != nil {
			return Release{}, err
		}

		for i := range list {
			gr := &list[i]
			if gr.Draft || !version.IsRelease(gr.TagName) || !ch.Receives(channelOf(gr.TagName, gr.Prerelease)) {
				continue
			}
			if best == nil || version.Newer(best.TagName, gr.TagName) {
				best = gr
			}
		}

		if best != nil || len(list) < listPageSize {
			break
		}
	}

	if best == nil {
		return Release{}, fmt.Errorf("update: GET %s: %w: channel %s, %d pages read", base, errNoListedRelease, ch, pages)
	}

	return best.release(), nil
}

// listPage reads one page of the REST API's release list from pageURL through client, the
// [GitHubSource.apiClient] of the lookup.
func (s *GitHubSource) listPage(ctx context.Context, client *http.Client, pageURL string) ([]gitHubRelease, error) {
	var list []gitHubRelease

	err := s.apiGet(ctx, client, pageURL, maxListResponseBody, func(body []byte) error {
		if err := json.Unmarshal(body, &list); err != nil {
			return fmt.Errorf("update: GET %s: decoding releases: %w", pageURL, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return list, nil
}

// apiGet makes the REST API request for rawURL through client, the [GitHubSource.apiClient] of the
// lookup, and hands decode the body of a success response, read up to limit bytes, or on a 304 Not
// Modified the body cached for rawURL. The request carries If-None-Match only when Cache holds an entry for rawURL. A
// success response carrying an ETag replaces that entry once its body is read, whatever decode
// returns, so a 304 answered later decodes to the same result; the write is skipped when ctx is
// done and its failure is ignored. Any other status is the error [GitHubSource.apiStatusError]
// returns, and a 304 with no entry is a [*StatusError].
func (s *GitHubSource) apiGet(ctx context.Context, client *http.Client, rawURL string, limit int64, decode func(body []byte) error) error {
	cached, hit := s.cacheGet(ctx, rawURL)

	var etag string
	if hit {
		etag = cached.ETag
	}

	var fresh *CachedResponse

	err := s.get(ctx, client, rawURL, etag, func(resp *http.Response) error {
		var body []byte

		switch {
		case resp.StatusCode == http.StatusNotModified && hit:
			body = cached.Body
		case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
			return s.apiStatusError(rawURL, resp)
		default:
			read, err := readLimited(resp.Body, limit)
			if err != nil {
				return fmt.Errorf("update: GET %s: %w", rawURL, err)
			}

			if tag := resp.Header.Get("ETag"); tag != "" {
				fresh = &CachedResponse{ETag: tag, Body: read}
			}

			body = read
		}

		return decode(body)
	})

	if fresh != nil && s.Cache != nil && ctx.Err() == nil {
		_ = s.Cache.Put(ctx, rawURL, *fresh)
	}

	return err
}

// cacheGet returns Cache's entry for rawURL, and false when Cache is nil or holds no usable entry:
// one without an ETag cannot make a request conditional.
func (s *GitHubSource) cacheGet(ctx context.Context, rawURL string) (CachedResponse, bool) {
	if s.Cache == nil {
		return CachedResponse{}, false
	}

	r, ok := s.Cache.Get(ctx, rawURL)
	if !ok || r.ETag == "" {
		return CachedResponse{}, false
	}

	return r, true
}

// get makes one GET request for rawURL through client, bounded by Timeout, and hands the
// response to read. A non-empty etag is sent as If-None-Match. The body is drained and closed
// after read returns, and every error is passed through [classifyError].
func (s *GitHubSource) get(ctx context.Context, client *http.Client, rawURL, etag string, read func(*http.Response) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("update: GET %s: %w", rawURL, err)
	}

	req.Header.Set("User-Agent", gitHubUserAgent)
	req.Header.Set("Accept", gitHubAccept)
	req.Header.Set("X-GitHub-Api-Version", gitHubAPIVersion)

	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return classifyError(rawURL, err)
	}
	defer drainAndClose(resp.Body)

	return classifyError(rawURL, read(resp))
}

// now returns the current time from Now, or from [time.Now] when Now is nil.
func (s *GitHubSource) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}

	return s.Now()
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

// tagFromLocation returns the tag a redirect from the latest-release page at requested names, or
// "" when it names none. The location's path must be the requested page's directory followed by
// /tag/<tag>, compared without regard to case, as GitHub matches owner and repository names; the
// tag is everything after it. A location with an empty tag names none.
func tagFromLocation(requested, location *url.URL) string {
	prefix := path.Dir(requested.Path) + "/tag/"

	// A path shorter than prefix, or equal to it, leaves nothing after the cut and so names no tag.
	cut := min(len(prefix), len(location.Path))
	if !strings.EqualFold(location.Path[:cut], prefix) {
		return ""
	}

	return location.Path[cut:]
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
	return readStatusError(rawURL, resp)
}

// readStatusError is statusError with the concrete type.
func readStatusError(rawURL string, resp *http.Response) *StatusError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))

	return newStatusError(rawURL, resp.StatusCode, string(body))
}

// apiStatusError returns the error for resp, a non-success response from the REST API at rawURL:
// a [*RateLimitError] for a 403 or 429 that reports an exhausted limit, a [*TokenRejectedError] for
// a 401 to a request that carried an Authorization header, and the plain [*StatusError] otherwise.
// Only API call sites use it; a download's 429 is an ordinary [*StatusError].
func (s *GitHubSource) apiStatusError(rawURL string, resp *http.Response) error {
	status := readStatusError(rawURL, resp)
	authenticated := resp.Request != nil && resp.Request.Header.Get("Authorization") != ""

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		if authenticated {
			return &TokenRejectedError{Status: status}
		}
	case http.StatusForbidden, http.StatusTooManyRequests:
		if rl := s.rateLimitError(resp.Header, authenticated, status); rl != nil {
			return rl
		}
	}

	return status
}

// rateLimitError returns the [*RateLimitError] the headers of a 403 or 429 describe, or nil when
// they report no exhausted limit: neither X-RateLimit-Remaining of 0 nor a Retry-After.
func (s *GitHubSource) rateLimitError(h http.Header, authenticated bool, status *StatusError) *RateLimitError {
	limit, _ := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, remainingErr := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	rl := &RateLimitError{Limit: limit, Remaining: remaining, Authenticated: authenticated, Status: status}

	switch {
	case remainingErr == nil && remaining == 0:
		if unix, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			rl.Reset = time.Unix(unix, 0).UTC()
		}
	case h.Get("Retry-After") != "":
		rl.Reset = s.retryAfter(h.Get("Retry-After"))
	default:
		return nil
	}

	return rl
}

// retryAfter returns the time a Retry-After value, delay seconds or an HTTP date, names, or the
// zero time when it is neither.
func (s *GitHubSource) retryAfter(value string) time.Time {
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil && secs >= 0 {
		return s.now().Add(time.Duration(secs) * time.Second).UTC()
	}

	if t, err := http.ParseTime(value); err == nil {
		return t.UTC()
	}

	return time.Time{}
}

// readLimited reads r to its end, failing when it holds more than limit bytes.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}

	return body, nil
}

// drainAndClose discards at most maxDrainBody bytes of body and closes it.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBody))
	_ = body.Close()
}
