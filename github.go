package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/mccutchen/ghavm/internal/slogctx"
)

// GitHubClient is a client for GitHub's REST and GraphQL APIs, which exposes
// the functionality needed to resolve versions, commits, refs.
type GitHubClient struct {
	httpClient *http.Client

	upgradeCache *Cache[string, UpgradeCandidates]
	versionCache *Cache[string, []string]
	refCache     *Cache[string, string]
}

// NewGitHubClient creates a new [GitHubClient] that will use the given
// token to authenticate both GraphQL and REST API requests.
//
// If non-nil, the given [http.Client] will be used after updating its
// transport to inject the correct auth header. Otherwise [http.DefaultClient]
// will be used.
func NewGitHubClient(ghToken string, httpClient *http.Client) *GitHubClient {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	httpClient.Transport = newAuthTransport(ghToken, httpClient.Transport)

	return &GitHubClient{
		httpClient: httpClient,

		upgradeCache: &Cache[string, UpgradeCandidates]{},
		versionCache: &Cache[string, []string]{},
		refCache:     &Cache[string, string]{},
	}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// doGraphql executes a GraphQL query using plain HTTP and un-marshals the
// response into target.
func (c *GitHubClient) doGraphql(ctx context.Context, queryString string, variables map[string]any, target any) error {
	reqBody := graphqlRequest{
		Query:     queryString,
		Variables: variables,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	slogctx.Debug(
		ctx, "github: graphql query",
		slog.Int("status", resp.StatusCode),
		slog.String("ratelimit.limit", resp.Header.Get("x-ratelimit-limit")),
		slog.String("ratelimit.remaining", resp.Header.Get("x-ratelimit-remaining")),
		slog.String("ratelimit.used", resp.Header.Get("x-ratelimit-used")),
		slog.String("ratelimit.reset", resp.Header.Get("x-ratelimit-reset")),
	)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("transport error: %s", resp.Status)
	}

	var gqlResp graphqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("query errors: %v", gqlResp.Errors)
	}
	if err := json.Unmarshal(gqlResp.Data, target); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}
	return nil
}

// doREST makes a REST API call to the GitHub API and un-marshals the response
// into the given target.
func (c *GitHubClient) doREST(ctx context.Context, method string, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+url, nil)
	if err != nil {
		panic("github: invalid URL: " + err.Error())
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failure: %w", err)
	}
	defer resp.Body.Close()
	slogctx.Debug(
		ctx, "github: http request",
		slog.String("method", method),
		slog.String("url", req.URL.String()),
		slog.Int("status", resp.StatusCode),
		slog.String("ratelimit.limit", resp.Header.Get("x-ratelimit-limit")),
		slog.String("ratelimit.remaining", resp.Header.Get("x-ratelimit-remaining")),
		slog.String("ratelimit.used", resp.Header.Get("x-ratelimit-used")),
		slog.String("ratelimit.reset", resp.Header.Get("x-ratelimit-reset")),
	)
	if resp.StatusCode >= 400 {
		switch resp.StatusCode {
		case 401:
			return errors.New("invalid auth token")
		case 403:
			return errors.New("access denied")
		default:
			return fmt.Errorf("http error: %s", resp.Status)
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}
	return nil
}

// GetUpgradeCandidates returns [UpgradeCandidates].
func (c *GitHubClient) GetUpgradeCandidates(ctx context.Context, targetRepo string, currentRelease Release) (UpgradeCandidates, error) {
	// if we have not identified the semver version for the current release,
	// we cannot meaningfully suggest upgrade versions, so we bail early
	if currentRelease.Version == "" {
		return UpgradeCandidates{}, nil
	}
	return c.upgradeCache.Do(ctx, cacheKey(targetRepo, currentRelease.Version), func() (UpgradeCandidates, error) {
		return c.doGetUpgradeCandidates(ctx, targetRepo, currentRelease)
	})
}

func (c *GitHubClient) doGetUpgradeCandidates(ctx context.Context, targetRepo string, currentRelease Release) (UpgradeCandidates, error) {
	var (
		currentMajorVersion     = semver.Major(currentRelease.Version)
		latestCompatibleRelease = Release{}
		latestRelease           = Release{}
	)

	for candidate, err := range c.iterAllReleases(ctx, targetRepo) {
		if err != nil {
			return UpgradeCandidates{}, fmt.Errorf("failed to gather candidate versions: %w", err)
		}
		// discard anything older than our current version
		if !isUpgradeCandidate(currentRelease.Version, candidate.Version) {
			break
		}
		// track latest release and latest compatible release w/ same major
		// version
		latestRelease = chooseNewestRelease(latestRelease, candidate)
		if semver.Major(candidate.Version) == currentMajorVersion {
			latestCompatibleRelease = chooseNewestRelease(latestCompatibleRelease, candidate)
		}
	}
	result := UpgradeCandidates{
		Latest:           latestRelease,
		LatestCompatible: latestCompatibleRelease,
	}
	return result, nil
}

// isUpgradeCandidate returns true if the candidate version is equal to or
// newer than the current version, according to semver rules.
//
// Note that we treat equal versions as "upgrade" candidates because it lets
// us easily handle the case where the current version is already the latest
// version.
func isUpgradeCandidate(currentVersion, candidateVersion string) bool {
	var (
		currentValid   = semver.IsValid(currentVersion)
		candidateValid = semver.IsValid(candidateVersion)
	)
	switch {
	case currentValid && candidateValid:
		return semver.Compare(currentVersion, candidateVersion) <= 0
	case candidateValid:
		// if current version is not semver but candidate is, treat candidate
		// as an upgrade
		return true
	default:
		// otherwise, candidate is not an upgrade
		return false
	}
}

// chooseNewestRelease returns whichever release is newer, according to semver
// rules.
func chooseNewestRelease(a, b Release) Release {
	if semver.Compare(a.Version, b.Version) == 1 {
		return a
	}
	return b
}

//go:embed graphql/getRepositoryReleases.graphql
var getRepositoryReleasesQuery string

type getRepositoryReleasesResp struct {
	Repository struct {
		Releases struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []struct {
				Tag struct {
					Target struct {
						OID    string `json:"oid"`
						Target struct {
							OID string `json:"oid"`
						} `json:"target"`
					} `json:"target"`
				} `json:"tag"`
				TagName string `json:"tagName"`
				URL     string `json:"url"`
			} `json:"nodes"`
		} `json:"releases"`
	} `json:"repository"`
}

// iterAllReleases returns in iter over all [Release]s in a repo.
func (c *GitHubClient) iterAllReleases(ctx context.Context, targetRepo string) iter.Seq2[Release, error] {
	return func(yield func(Release, error) bool) {
		owner, repo, ok := strings.Cut(targetRepo, "/")
		if !ok {
			yield(Release{}, fmt.Errorf("targetRepo must be specified in \"owner/repo\" format, got %q", targetRepo))
			return
		}
		variables := map[string]any{
			"owner":  owner,
			"repo":   repo,
			"cursor": "",
		}
		for {
			var resp getRepositoryReleasesResp
			if err := c.doGraphql(ctx, getRepositoryReleasesQuery, variables, &resp); err != nil {
				yield(Release{}, fmt.Errorf("graphql error: %w", err))
				return
			}
			for _, release := range resp.Repository.Releases.Nodes {
				// check for a match in the direct commit OID (for
				// "lightweight" tags) or the nested commit OID (for
				// "annotated" tags)
				commit := release.Tag.Target.OID
				if release.Tag.Target.Target.OID != "" {
					commit = release.Tag.Target.Target.OID
				}
				release := Release{
					Version:    release.TagName,
					CommitHash: commit,
				}
				if !yield(release, nil) {
					return
				}
			}
			if !resp.Repository.Releases.PageInfo.HasNextPage {
				break
			}
			variables["cursor"] = resp.Repository.Releases.PageInfo.EndCursor
		}
	}
}

//go:embed graphql/getVersionTagsForRef.graphql
var getVersionTagsForRefQuery string

type versionTagsForRefResp struct {
	Repository struct {
		Refs struct {
			Nodes []struct {
				Name   string
				Target struct {
					Oid    string `json:"oid"`
					Target struct {
						Oid string `json:"oid"`
					} `json:"target"`
				}
			}
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"refs"`
	} `json:"repository"`
}

// GetVersionTagsForCommitHash returns any semver-compatible tags pointing to the
// given commit hash.
func (c *GitHubClient) GetVersionTagsForCommitHash(ctx context.Context, targetRepo string, commitHash string) ([]string, error) {
	return c.versionCache.Do(ctx, cacheKey(targetRepo, commitHash), func() ([]string, error) {
		return c.doGetVersionTagsForHash(ctx, targetRepo, commitHash)
	})
}

func (c *GitHubClient) doGetVersionTagsForHash(ctx context.Context, targetRepo string, commitHash string) ([]string, error) {
	owner, repo, ok := strings.Cut(targetRepo, "/")
	if !ok {
		return nil, fmt.Errorf("targetRepo must be specified in \"owner/repo\" format, got %q", targetRepo)
	}

	var tags []string
	variables := map[string]any{
		"owner":  owner,
		"repo":   repo,
		"cursor": "",
	}
	for {
		var resp versionTagsForRefResp
		if err := c.doGraphql(ctx, getVersionTagsForRefQuery, variables, &resp); err != nil {
			return nil, fmt.Errorf("graphql error: %w", err)
		}
		for _, node := range resp.Repository.Refs.Nodes {
			if !semver.IsValid(node.Name) {
				continue
			}
			// check for a match in the direct commit OID (for "lightweight"
			// tags) or the nested commit OID (for "annotated" tags)
			if node.Target.Oid == commitHash || node.Target.Target.Oid == commitHash {
				tags = append(tags, node.Name)
			}
		}
		if !resp.Repository.Refs.PageInfo.HasNextPage {
			break
		}
		variables["cursor"] = resp.Repository.Refs.PageInfo.EndCursor
	}
	// return any matching version tags in descending order, with the newest
	// and most specific semver tag first
	semver.Sort(tags)
	slices.Reverse(tags)
	return tags, nil
}

// GetCommitHashForRef returns the full SHA commit hash for the given ref,
// which may be a (possibly shortened) commit hash, a branch name, or a tag
// name.
func (c *GitHubClient) GetCommitHashForRef(ctx context.Context, targetRepo string, ref string) (string, error) {
	return c.refCache.Do(ctx, cacheKey(targetRepo, ref), func() (string, error) {
		return c.doGetCommitHashForRef(ctx, targetRepo, ref)
	})
}

func (c *GitHubClient) doGetCommitHashForRef(ctx context.Context, targetRepo string, ref string) (string, error) {
	owner, repo, ok := strings.Cut(targetRepo, "/")
	if !ok {
		return "", fmt.Errorf("targetRepo must be specified in \"owner/repo\" format, got %q", targetRepo)
	}

	// Note: we check whether the ref is a (possibly short) commit hash,
	// branch name, or tag name, in that order.
	//
	// So from here down, we're checking whether we *didn't* get an error as an
	// indication that we successfully looked up the object and can return
	// early.

	// potentially a (shortened?) commit hash
	if isHex(ref) {
		var commit gitCommitResponse
		if err := c.doREST(ctx, "GET", fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, ref), &commit); err == nil {
			return commit.SHA, nil
		}
	}

	// potentially a branch
	var gitRef gitRefResponse
	if err := c.doREST(ctx, "GET", fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", owner, repo, ref), &gitRef); err == nil {
		return gitRef.Object.SHA, nil
	}

	// potentially a tag
	if err := c.doREST(ctx, "GET", fmt.Sprintf("/repos/%s/%s/git/ref/tags/%s", owner, repo, ref), &gitRef); err == nil {
		// lightweight tag, we're done
		if gitRef.Object.Type == "commit" {
			return gitRef.Object.SHA, nil
		}
		// need another request for annotated tags
		if err := c.doREST(ctx, "GET", fmt.Sprintf("/repos/%s/%s/git/tags/%s", owner, repo, gitRef.Object.SHA), &gitRef); err == nil {
			return gitRef.Object.SHA, nil
		}
	}

	return "", fmt.Errorf("failed to resolve reference %s", ref)
}

// ValidateAuth ensures that the configured auth token is valid by fetching
// info on the authenticated user.
func (c *GitHubClient) ValidateAuth(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := c.doREST(ctx, "GET", "/user", &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

type gitCommitResponse struct {
	SHA string `json:"sha"`
}

type gitRefResponse struct {
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}

var (
	hexPattern = regexp.MustCompile(`^[A-Fa-f0-9]+$`)
	isHex      = hexPattern.MatchString
)

func cacheKey(s ...string) string {
	return strings.Join(s, "/")
}

// authTransport is an http.RoundTripper that adds GitHub authentication
// to outbound requests by injecting a Bearer token in the Authorization header.
type authTransport struct {
	token     string
	transport http.RoundTripper
}

// newAuthTransport creates a new authTransport with the given token.
// If transport is nil, http.DefaultTransport is used.
func newAuthTransport(token string, transport http.RoundTripper) *authTransport {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &authTransport{
		token:     token,
		transport: transport,
	}
}

// RoundTrip implements http.RoundTripper by adding the Authorization header
// and delegating to the underlying transport.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	reqCopy := req.Clone(req.Context())
	reqCopy.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(reqCopy)
}
