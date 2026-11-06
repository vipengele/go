package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxAPIRedirects is the most redirects the REST API client follows when the injected client sets
// no CheckRedirect of its own: the limit [http.Client] applies by default.
const maxAPIRedirects = 10

// errTooManyAPIRedirects stops the REST API client after maxAPIRedirects redirects.
var errTooManyAPIRedirects = errors.New("stopped after 10 redirects")

// CrossHostRedirectError reports a REST API response that redirected to a host other than the
// API's own. The redirect is not followed, so the token never reaches that host.
type CrossHostRedirectError struct {
	// URL is the address of the request that answered with the redirect.
	URL string
	// Location is the address the redirect names.
	Location string
}

// Error implements error.
func (e *CrossHostRedirectError) Error() string {
	return fmt.Sprintf("update: GET %s: refusing redirect to another host: %s", e.URL, e.Location)
}

// tokenFromEnv returns a token provider that reads GH_TOKEN through getenv, then GITHUB_TOKEN when
// GH_TOKEN is empty. Both empty gives "", meaning unauthenticated requests.
func tokenFromEnv(getenv func(string) string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if token := getenv("GH_TOKEN"); token != "" {
			return token, nil
		}

		return getenv("GITHUB_TOKEN"), nil
	}
}

// apiClient returns the client the REST API requests of one Latest call go through: a shallow
// copy of the injected client whose transport adds "Authorization: Bearer <token>" to requests
// for the APIURL host alone, and whose CheckRedirect refuses a redirect to any other host with a
// [*CrossHostRedirectError]. A same-host redirect goes to the injected client's CheckRedirect, or
// stops after 10 redirects when it has none. The injected client, its transport and the request
// are never modified.
//
// Token is called here, so only a request to the REST API needs it, and a provider error fails
// that request alone. An empty token sends no Authorization header.
func (s *GitHubSource) apiClient(ctx context.Context) (*http.Client, error) {
	token, err := s.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("update: reading the GitHub token: %w", err)
	}

	base := s.client()
	apiHost := hostOf(s.apiURL())

	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	next := base.CheckRedirect
	client := *base
	client.Transport = &tokenTransport{base: transport, host: apiHost, token: token}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameHost(req.URL.Host, apiHost) {
			return &CrossHostRedirectError{URL: via[len(via)-1].URL.String(), Location: req.URL.String()}
		}

		if next != nil {
			return next(req, via)
		}

		if len(via) >= maxAPIRedirects {
			return errTooManyAPIRedirects
		}

		return nil
	}

	return &client, nil
}

// token returns the result of Token, or "" when Token is nil.
func (s *GitHubSource) token(ctx context.Context) (string, error) {
	if s.Token == nil {
		return "", nil
	}

	return s.Token(ctx)
}

// tokenTransport forwards every request to base, adding the bearer token to a clone of each
// request for host. A request for any other host, and every request when token is empty, is
// forwarded as it is.
type tokenTransport struct {
	base  http.RoundTripper
	host  string
	token string
}

// RoundTrip implements [http.RoundTripper].
func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token == "" || !sameHost(req.URL.Host, t.host) {
		return t.base.RoundTrip(req)
	}

	authed := req.Clone(req.Context())
	authed.Header.Set("Authorization", "Bearer "+t.token)

	return t.base.RoundTrip(authed)
}

// hostOf returns the host, with any port, of rawURL, or "" when it does not parse.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return u.Host
}

// sameHost reports whether host is apiHost, compared without regard to case as DNS names are. An
// empty apiHost matches nothing.
func sameHost(host, apiHost string) bool {
	return apiHost != "" && strings.EqualFold(host, apiHost)
}
