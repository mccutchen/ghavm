package ghavm

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mccutchen/ghavm/internal/slogctx"
	"github.com/mccutchen/ghavm/internal/testing/assert"
	"github.com/mccutchen/ghavm/internal/testing/must"
)

func TestNew(t *testing.T) {
	client := NewGitHubClient("token", nil)
	if client.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func testCtx() context.Context {
	return slogctx.New(
		context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestGetUpgradeCandidates(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		targetRepo     string
		currentRelease Release
		gqlEndpoints   map[string]httpResponse
		expected       UpgradeCandidates
		expectError    error
	}{
		"invalid repo format": {
			targetRepo: "invalid-format",
			currentRelease: Release{
				Version:    "1.0.0",
				CommitHash: "currenthash",
			},
			expectError: errors.New("failed to gather candidate versions: targetRepo must be specified in \"owner/repo\" format, got \"invalid-format\""),
		},
		"called without current version resolved": {
			targetRepo: "owner/repo",
			currentRelease: Release{
				CommitHash: "currenthash",
			},
			gqlEndpoints: map[string]httpResponse{
				// no endpoints because we should short circuit and make no
				// requests if we have no current version to compare
			},
			expected: UpgradeCandidates{},
		},
		"already at latest release": {
			targetRepo:     "owner/repo",
			currentRelease: Release{Version: "v2.0.0", CommitHash: "currenthash"},
			gqlEndpoints: map[string]httpResponse{
				"d20dbd468b": okResponse(`{
					"data": {
						"repository": {
							"releases": {
								"pageInfo": {
									"hasNextPage": false,
									"endCursor": ""
								},
								"nodes": [
									{
										"tag": {
											"target": {
												"oid": "currenthash"
											}
										},
										"tagName": "v2.0.0",
										"url": "https://github.com/owner/repo/releases/tag/v1.1.0"
									},
									{
										"tag": {
											"target": {
												"oid": "abcdef123456"
											}
										},
										"tagName": "v1.0.0",
										"url": "https://github.com/owner/repo/releases/tag/v1.0.0"
									}
								]
							}
						}
					}
				}`),
			},
			expected: UpgradeCandidates{
				LatestCompatible: Release{
					Version:    "v2.0.0",
					CommitHash: "currenthash",
				},
				Latest: Release{
					Version:    "v2.0.0",
					CommitHash: "currenthash",
				},
			},
		},
		"compatible and major upgrades available": {
			targetRepo:     "owner/repo",
			currentRelease: Release{Version: "v1.0.0", CommitHash: "currenthash"},
			gqlEndpoints: map[string]httpResponse{
				"d20dbd468b": okResponse(`{
						"data": {
							"repository": {
								"releases": {
									"pageInfo": {
										"hasNextPage": false,
										"endCursor": ""
									},
									"nodes": [
										{
											"tag": {
												"target": {
													"oid": "aaa111"
												}
											},
											"tagName": "v2.0.0",
											"url": "https://github.com/owner/repo/releases/tag/v2.0.0"
										},
										{
											"tag": {
												"target": {
													"oid": "bbb222"
												}
											},
											"tagName": "v1.2.0",
											"url": "https://github.com/owner/repo/releases/tag/v1.2.0"
										},
										{
											"tag": {
												"target": {
													"oid": "ccc333"
												}
											},
											"tagName": "v1.1.0",
											"url": "https://github.com/owner/repo/releases/tag/v1.1.0"
										},
										{
											"tag": {
												"target": {
													"oid": "differenthash"
												}
											},
											"tagName": "v1.0.0",
											"url": "https://github.com/owner/repo/releases/tag/v1.0.0"
										}
									]
								}
							}
						}
					}`),
			},
			expected: UpgradeCandidates{
				Latest: Release{
					Version:    "v2.0.0",
					CommitHash: "aaa111",
				},
				LatestCompatible: Release{
					Version:    "v1.2.0",
					CommitHash: "bbb222",
				},
			},
		},
		"annotated tag handling": {
			targetRepo: "owner/repo",
			currentRelease: Release{
				Version:    "v1.0.0",
				CommitHash: "currenthash",
			},
			gqlEndpoints: map[string]httpResponse{
				"d20dbd468b": okResponse(`{
				  "data": {
				    "repository": {
				      "releases": {
				        "pageInfo": {
				          "hasNextPage": false,
				          "endCursor": ""
				        },
				        "nodes": [
				          {
				            "tag": {
				              "target": {
				                "oid": "lightweight1234",
				                "target": {
				                  "oid": "annotated456"
				                }
				              }
				            },
				            "tagName": "v1.1.0",
				            "url": "https://github.com/owner/repo/releases/tag/v1.1.0"
				          },
				          {
				            "tag": {
				              "target": {
				                "oid": "currenthash"
				              }
				            },
				            "tagName": "v1.0.0",
				            "url": "https://github.com/owner/repo/releases/tag/v1.0.0"
				          }
				        ]
				      }
				    }
				  }
				}`),
			},
			expected: UpgradeCandidates{
				Latest: Release{
					Version:    "v1.1.0",
					CommitHash: "annotated456",
				},
				LatestCompatible: Release{
					Version:    "v1.1.0",
					CommitHash: "annotated456",
				},
			},
		},
		"multiple pages": {
			targetRepo:     "owner/repo",
			currentRelease: Release{Version: "v1.0.0", CommitHash: "currenthash"},
			gqlEndpoints: map[string]httpResponse{
				"d20dbd468b": okResponse(`{
						"data": {
							"repository": {
								"releases": {
									"pageInfo": {
										"hasNextPage": true,
										"endCursor": "cursor1"
									},
									"nodes": [
										{
											"tag": {
												"target": {
													"oid": "aaa111"
												}
											},
											"tagName": "v2.0.0",
											"url": "https://github.com/owner/repo/releases/tag/v2.0.0"
										}
									]
								}
							}
						}
					}`),
				"30f6d0ee9a": okResponse(`{
						"data": {
							"repository": {
								"releases": {
									"pageInfo": {
										"hasNextPage": true,
										"endCursor": "cursor2"
									},
									"nodes": [
										{
											"tag": {
												"target": {
													"oid": "bbb222"
												}
											},
											"tagName": "v1.2.0",
											"url": "https://github.com/owner/repo/releases/tag/v1.2.0"
										}
									]
								}
							}
						}
					}`),
				"15b363e95e": okResponse(`{
						"data": {
							"repository": {
								"releases": {
									"pageInfo": {
										"hasNextPage": false,
										"endCursor": ""
									},
									"nodes": [
										{
											"tag": {
												"target": {
													"oid": "currenthash"
												}
											},
											"tagName": "v1.0.0",
											"url": "https://github.com/owner/repo/releases/tag/v1.0.0"
										}
									]
								}
							}
						}
					}`),
			},
			expected: UpgradeCandidates{
				Latest: Release{
					Version:    "v2.0.0",
					CommitHash: "aaa111",
				},
				LatestCompatible: Release{
					Version:    "v1.2.0",
					CommitHash: "bbb222",
				},
			},
		},
		"graphql error": {
			targetRepo:     "owner/repo",
			currentRelease: Release{Version: "v1.0.0", CommitHash: "currenthash"},
			gqlEndpoints: map[string]httpResponse{
				"d20dbd468b": okResponse(`{"errors": [{"message": "API error"}]}`),
			},
			expectError: errors.New("failed to gather candidate versions: graphql error: query errors: [{API error}]"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, tc.gqlEndpoints, nil)
			candidates, err := client.GetUpgradeCandidates(testCtx(), tc.targetRepo, tc.currentRelease)
			if tc.expectError != nil {
				assert.Error(t, err, tc.expectError)
			} else {
				assert.NilError(t, err)
				assert.Equal(t, candidates, tc.expected, "incorrect candidates")
			}
		})
	}
}

func TestGetVersionTagsForHash(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		targetRepo   string
		commitHash   string
		gqlEndpoints map[string]httpResponse
		expected     []string
		expectError  error
	}{
		"invalid repo format": {
			targetRepo:  "invalid-format",
			commitHash:  "abcdef123456",
			expectError: errors.New("targetRepo must be specified in \"owner/repo\" format, got \"invalid-format\""),
		},
		"no matching tags": {
			targetRepo: "owner/repo",
			commitHash: "abcdef123456",
			gqlEndpoints: map[string]httpResponse{
				"2590b2f6ce": okResponse(`{
					"data": {
						"repository": {
							"refs": {
								"nodes": [
									{
										"name": "v1.0.0",
										"target": {
											"oid": "differenthash"
										}
									}
								],
								"pageInfo": {
									"hasNextPage": false,
									"endCursor": ""
								}
							}
						}
					}
				}`),
			},
			expected: nil,
		},
		"one matching tag": {
			targetRepo: "owner/repo",
			commitHash: "abcdef123456",
			gqlEndpoints: map[string]httpResponse{
				"2590b2f6ce": okResponse(`{
					"data": {
						"repository": {
							"refs": {
								"nodes": [
									{
										"name": "v1.0.0",
										"target": {
											"oid": "abcdef123456"
										}
									},
									{
										"name": "not-semver",
										"target": {
											"oid": "abcdef123456"
										}
									}
								],
								"pageInfo": {
									"hasNextPage": false,
									"endCursor": ""
								}
							}
						}
					}
				}`),
			},
			expected: []string{"v1.0.0"},
		},
		"multiple matching tags": {
			targetRepo: "owner/repo",
			commitHash: "abcdef123456",
			gqlEndpoints: map[string]httpResponse{
				"2590b2f6ce": okResponse(`{
					"data": {
						"repository": {
							"refs": {
								"nodes": [
									{
										"name": "v1",
										"target": {
											"oid": "abcdef123456"
										}
									},
									{
										"name": "v1.0.0",
										"target": {
											"oid": "abcdef123456"
										}
									},
									{
										"name": "v1.0.1-prerelease",
										"target": {
											"oid": "abcdef123456"
										}
									},
									{
										"name": "v1.0.1",
										"target": {
											"oid": "abcdef123456"
										}
									}
								],
								"pageInfo": {
									"hasNextPage": false,
									"endCursor": ""
								}
							}
						}
					}
				}`),
			},
			expected: []string{
				"v1.0.1",
				"v1.0.1-prerelease",
				"v1.0.0",
				"v1",
			},
		},
		"multiple pages with tags": {
			targetRepo: "owner/repo",
			commitHash: "abcdef123456",
			gqlEndpoints: map[string]httpResponse{
				"2590b2f6ce": okResponse(`{
					"data": {
						"repository": {
							"refs": {
								"nodes": [
									{
										"name": "v1.0.0",
										"target": {
											"oid": "abcdef123456"
										}
									}
								],
								"pageInfo": {
									"hasNextPage": true,
									"endCursor": "cursor1"
								}
							}
						}
					}
				}`),
				"3b192be14c": okResponse(`{
					"data": {
						"repository": {
							"refs": {
								"nodes": [
									{
										"name": "v1.0.1",
										"target": {
											"oid": "abcdef123456"
										}
									}
								],
								"pageInfo": {
									"hasNextPage": false,
									"endCursor": ""
								}
							}
						}
					}
				}`),
			},
			expected: []string{"v1.0.1", "v1.0.0"},
		},
		"graphql error for version tags": {
			targetRepo: "owner/repo",
			commitHash: "abcdef123456",
			gqlEndpoints: map[string]httpResponse{
				"2590b2f6ce": okResponse(`{"errors": [{"message": "API error"}]}`),
			},
			expectError: errors.New("graphql error: query errors: [{API error}]"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, tc.gqlEndpoints, nil)
			tags, err := client.GetVersionTagsForCommitHash(testCtx(), tc.targetRepo, tc.commitHash)
			if tc.expectError != nil {
				assert.Error(t, err, tc.expectError)
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, tags, tc.expected, "version tags")
		})
	}
}

func TestGetCommitHashForRef(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		targetRepo      string
		ref             string
		restEndpoints   map[string]httpResponse
		expectedCommit  string
		expectError     error
		expectedAPIURLs []string
	}{
		"invalid repo format": {
			targetRepo:  "invalid-format",
			ref:         "v1.0.0",
			expectError: errors.New("targetRepo must be specified in \"owner/repo\" format, got \"invalid-format\""),
		},
		"full length commit hash": {
			targetRepo: "owner/repo",
			ref:        "0123456789abcdef0123456789abcdef01234567",
			restEndpoints: map[string]httpResponse{
				"GET /repos/owner/repo/commits/0123456789abcdef0123456789abcdef01234567": {
					status: 200,
					body: `{
						"sha": "0123456789abcdef0123456789abcdef01234567"
					}`,
				},
			},
			expectedCommit: "0123456789abcdef0123456789abcdef01234567",
		},
		"short commit hash": {
			targetRepo: "owner/repo",
			ref:        "0123456",
			restEndpoints: map[string]httpResponse{
				"GET /repos/owner/repo/commits/0123456": {
					status: 200,
					body: `{
						"sha": "0123456789abcdef0123456789abcdef01234567"
					}`,
				},
			},
			expectedCommit: "0123456789abcdef0123456789abcdef01234567",
		},
		"branch name": {
			targetRepo: "owner/repo",
			ref:        "main",
			restEndpoints: map[string]httpResponse{
				"GET /repos/owner/repo/git/ref/heads/main": {
					status: 200,
					body: `{
						"object": {
							"sha": "0123456789abcdef0123456789abcdef01234567",
							"type": "commit"
						}
					}`,
				},
			},
			expectedCommit: "0123456789abcdef0123456789abcdef01234567",
		},
		"tag name exists": {
			targetRepo: "owner/repo",
			ref:        "v1.0.0",
			restEndpoints: map[string]httpResponse{
				"GET /repos/owner/repo/git/ref/heads/v1.0.0": {
					status: 404,
					body:   `{"message": "Not Found"}`,
				},
				"GET /repos/owner/repo/git/ref/tags/v1.0.0": {
					status: 200,
					body: `{
						"object": {
							"sha": "0123456789abcdef0123456789abcdef01234567",
							"type": "commit"
						}
					}`,
				},
			},
			expectedCommit: "0123456789abcdef0123456789abcdef01234567",
		},
		"ref not found": {
			targetRepo: "owner/repo",
			ref:        "nonexistent",
			restEndpoints: map[string]httpResponse{
				"GET /repos/owner/repo/git/ref/heads/nonexistent": {
					status: 404,
					body:   `{"message": "Not Found"}`,
				},
				"GET /repos/owner/repo/git/ref/tags/nonexistent": {
					status: 404,
					body:   `{"message": "Not Found"}`,
				},
			},
			expectError: errors.New("failed to resolve reference nonexistent"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, nil, tc.restEndpoints)
			hash, err := client.GetCommitHashForRef(testCtx(), tc.targetRepo, tc.ref)
			if tc.expectError != nil {
				assert.Error(t, err, tc.expectError)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, hash, tc.expectedCommit, "unexpected commit hash")
		})
	}
}

func TestValidateAuth(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		restEndpoints map[string]httpResponse
		expectLogin   string
		expectError   error
	}{
		"auth okay": {
			expectLogin: "test-user",
			restEndpoints: map[string]httpResponse{
				"GET /user": okResponse(`{
							"login": "test-user",
							"id": 1234,
							"name": "Test User",
							"email": "test-user@example.com"
						}`),
			},
		},
		"invalid token": {
			expectError: errors.New("invalid auth token"),
			restEndpoints: map[string]httpResponse{
				"GET /user": errResponse(http.StatusUnauthorized, ""),
			},
		},
		"forbidden": {
			expectError: errors.New("access denied"),
			restEndpoints: map[string]httpResponse{
				"GET /user": errResponse(http.StatusForbidden, ""),
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, nil, tc.restEndpoints)
			login, err := client.ValidateAuth(testCtx())
			if tc.expectError != nil {
				assert.Error(t, err, tc.expectError)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, login, tc.expectLogin, "incorrect login")
		})
	}
}

func TestReleaseExists(t *testing.T) {
	t.Parallel()
	var (
		r1 = Release{}
		r2 = Release{
			Version:    "v1.0.0",
			CommitHash: "abcdef123456",
		}
	)
	assert.Equal(t, r1.Exists(), false, "r1 should not exist")
	assert.Equal(t, r2.Exists(), true, "r2 should exist")
}

func TestIsUpgradeCandidate(t *testing.T) {
	t.Parallel()
	upgradeCases := []struct {
		current   string
		candidate string
		expected  bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.1", "v1.0.0", false},
		{"v2.0.0", "v1.0.0", false},

		// same version is considered an upgrade candidate
		{"v1.0.0", "v1.0.0", true},

		// a semver is always considered newer than a non-semver
		{"main", "v1.0.0", true},
		{"v1.0.0", "main", false},
		{"main1", "main2", false},
	}

	for _, tc := range upgradeCases {
		t.Run(fmt.Sprintf("isUpgradeCandidate(%s,%s)", tc.current, tc.candidate), func(t *testing.T) {
			result := isUpgradeCandidate(tc.current, tc.candidate)
			assert.Equal(t, result, tc.expected, "is upgrade candidate?")
		})
	}
}

func TestChooseNewestRelease(t *testing.T) {
	t.Parallel()
	releaseCases := map[string]struct {
		a        Release
		b        Release
		expected Release
	}{
		"a is newer": {
			a:        Release{Version: "v2.0.0", CommitHash: "abc"},
			b:        Release{Version: "v1.0.0", CommitHash: "def"},
			expected: Release{Version: "v2.0.0", CommitHash: "abc"},
		},
		"b is newer": {
			a:        Release{Version: "v1.0.0", CommitHash: "abc"},
			b:        Release{Version: "v2.0.0", CommitHash: "def"},
			expected: Release{Version: "v2.0.0", CommitHash: "def"},
		},
		"same version (b wins)": {
			a:        Release{Version: "v1.0.0", CommitHash: "abc"},
			b:        Release{Version: "v1.0.0", CommitHash: "def"},
			expected: Release{Version: "v1.0.0", CommitHash: "def"},
		},
	}
	for name, tc := range releaseCases {
		t.Run(name, func(t *testing.T) {
			result := chooseNewestRelease(tc.a, tc.b)
			assert.Equal(t, result, tc.expected, "choose newest release")
		})
	}
}

// newTestClient returns a [GitHubClient] whose underlying http transport is hijacked
// to point to an httptest.Server that will expose the given graphql and rest
// endpoints, which will return canned responses.
//
// Each graphql endpoint is identified by the sha256 hash of the graphql query
// received in the request body. Each rest enpdoint is identified by the
// combination of request method and URL.
//
// See [graphqlSig] and [restSig] for details.
func newTestClient(t testing.TB, graphqlEndpoints map[string]httpResponse, restEndpoints map[string]httpResponse) *GitHubClient {
	t.Helper()
	const fakeAuthToken = "fake-auth-token" // #nosec G101
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Header.Get("Authorization"), "Bearer "+fakeAuthToken, "incorrect Authorization header")
		switch r.URL.Path {
		case "/graphql":
			assert.Equal(t, r.Method, http.MethodPost, "incorrect graphql request method")
			sig := graphqlSig(t, r)
			resp, ok := graphqlEndpoints[sig]
			if !ok {
				t.Fatalf("no response for graphql request signature %q", sig)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if resp.status != 0 {
				w.WriteHeader(resp.status)
			}
			fprintln(w, resp.body)
		default:
			sig := restSig(t, r)
			resp, ok := restEndpoints[sig]
			if !ok {
				t.Fatalf("no response for rest request %q", sig)
			}
			w.Header().Set("Content-Type", "application/json")
			if resp.status != 0 {
				w.WriteHeader(resp.status)
			}
			fprintln(w, resp.body)
		}
	}))
	t.Cleanup(srv.Close)

	httpClient := &http.Client{
		Transport: &fakeTransport{
			url: srv.URL,
		},
	}
	return NewGitHubClient(fakeAuthToken, httpClient)
}

func graphqlSig(t testing.TB, r *http.Request) string {
	body := must.ReadAll(t, r.Body)
	r.Body = io.NopCloser(strings.NewReader(body))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(body)))[:10]
}

func restSig(t testing.TB, r *http.Request) string {
	t.Helper()
	return r.Method + " " + r.URL.String()
}

type fakeTransport struct {
	url string
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := *req
	newReq.URL.Scheme = "http"
	newReq.URL.Host = strings.TrimPrefix(f.url, "http://")
	return http.DefaultTransport.RoundTrip(&newReq)
}

type httpResponse struct {
	status int
	body   string
}

func okResponse(body string) httpResponse {
	return httpResponse{http.StatusOK, body}
}

func errResponse(code int, body string) httpResponse {
	return httpResponse{code, body}
}
