package ghavm

import (
	"path"
	"testing"

	"github.com/mccutchen/ghavm/internal/testing/assert"
)

func TestMaybeParseAction(t *testing.T) {
	testCases := []struct {
		line string
		want Action
	}{
		{
			line: "      uses: owner/repo@v1.2.3",
			want: Action{
				Name: "owner/repo",
				Ref:  "v1.2.3",
			},
		},
		{
			line: "   - uses: owner/repo@v1.2.3",
			want: Action{
				Name: "owner/repo",
				Ref:  "v1.2.3",
			},
		},
		{
			line: "uses: owner/repo@v1.2.3  # trailing comments are ignored",
			want: Action{
				Name: "owner/repo",
				Ref:  "v1.2.3",
			},
		},
		{
			line: "uses:owner/repo@v1.2.3#whitespace is optional",
			want: Action{
				Name: "owner/repo",
				Ref:  "v1.2.3",
			},
		},

		// edge case `uses:` declarations
		{
			line: "uses: slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@v1.4.0",
			want: Action{
				Name: "slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml",
				Ref:  "v1.4.0",
			},
		},
		{
			line: "uses: mccutchen/ghavm-test-repo/sub-workflow@v2.2.2",
			want: Action{
				Name: "mccutchen/ghavm-test-repo/sub-workflow",
				Ref:  "v2.2.2",
			},
		},
		{
			line: "uses: owner/repo/.github/workflows/workflow.yaml@v1.0.0",
			want: Action{
				Name: "owner/repo/.github/workflows/workflow.yaml",
				Ref:  "v1.0.0",
			},
		},
		{
			line: "uses: owner/repo/path/to/action@main",
			want: Action{
				Name: "owner/repo/path/to/action",
				Ref:  "main",
			},
		},

		// testing a variety of ref formats we need to support
		{
			line: "uses: owner/repo@abcd1234",
			want: Action{
				Name: "owner/repo",
				Ref:  "abcd1234",
			},
		},
		{
			line: "uses: owner/repo@4c7fcab669655251627f630be05d37d7396039be",
			want: Action{
				Name: "owner/repo",
				Ref:  "4c7fcab669655251627f630be05d37d7396039be",
			},
		},
		{
			line: "uses: owner/repo@main",
			want: Action{
				Name: "owner/repo",
				Ref:  "main",
			},
		},
		{
			line: "uses: owner/repo@feature/re_name-01 # complex branch name",
			want: Action{
				Name: "owner/repo",
				Ref:  "feature/re_name-01",
			},
		},
		{
			line: "uses: owner/repo@1.2.3 # not quite semver",
			want: Action{
				Name: "owner/repo",
				Ref:  "1.2.3",
			},
		},

		// negative test cases
		{
			// commented out lines are ignored
			line: "#   uses: owner/repo@v1.2.3",
			want: Action{},
		},
		{
			// malformed ref (two @ symbols)
			line: "uses: owner/repo@v1.2.3@foo # malformed ref",
			want: Action{},
		},
		{
			// local workflow definitions
			line: "uses: ./.github/actions/custom-action",
			want: Action{},
		},
		{
			// docker images
			line: "uses: docker://mccutchen/ghavm-test-repo:2.2.2",
			want: Action{},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			got := maybeParseAction(tc.line)
			assert.Equal(t, got, tc.want, "incorrect result")
		})
	}
}

func TestScanFileFiltering(t *testing.T) {
	testCases := map[string]struct {
		opts     scanOpts
		expected []string
	}{
		"no filtering": {
			opts:     scanOpts{},
			expected: []string{"actions/setup-go", "actions/checkout", "golangci/golangci-lint-action", "codecov/codecov-action"},
		},
		"selects only": {
			opts:     scanOpts{Selects: []string{"actions/checkout", "codecov/codecov-action"}},
			expected: []string{"actions/checkout", "codecov/codecov-action"},
		},
		"excludes only": {
			opts:     scanOpts{Excludes: []string{"actions/setup-go", "golangci/golangci-lint-action"}},
			expected: []string{"actions/checkout", "codecov/codecov-action"},
		},
		"excludes take precedence over selects": {
			opts:     scanOpts{Selects: []string{"actions/checkout", "actions/setup-go"}, Excludes: []string{"actions/checkout"}},
			expected: []string{"actions/setup-go"},
		},
		"exclude all": {
			opts:     scanOpts{Excludes: []string{"actions/setup-go", "actions/checkout", "golangci/golangci-lint-action", "codecov/codecov-action"}},
			expected: []string{},
		},
		"target wildcard": {
			opts:     scanOpts{Selects: []string{"actions/*"}},
			expected: []string{"actions/setup-go", "actions/checkout"},
		},
		"exclude wildcard": {
			opts:     scanOpts{Excludes: []string{"actions/*"}},
			expected: []string{"golangci/golangci-lint-action", "codecov/codecov-action"},
		},
		"mixed exact and wildcard targets": {
			opts:     scanOpts{Selects: []string{"actions/*", "codecov/codecov-action"}},
			expected: []string{"actions/setup-go", "actions/checkout", "codecov/codecov-action"},
		},
		"mixed exact and wildcard excludes": {
			opts:     scanOpts{Excludes: []string{"actions/*", "codecov/codecov-action"}},
			expected: []string{"golangci/golangci-lint-action"},
		},
		"wildcard target with exact exclude": {
			opts:     scanOpts{Selects: []string{"actions/*"}, Excludes: []string{"actions/checkout"}},
			expected: []string{"actions/setup-go"},
		},
		"wildcard exclude takes precedence over wildcard target": {
			opts:     scanOpts{Selects: []string{"actions/*"}, Excludes: []string{"actions/*"}},
			expected: []string{},
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			workflow, err := scanFile(path.Join("testdata", "example.yaml"), tc.opts)
			assert.NilError(t, err)

			actualNames := make([]string, 0, len(workflow.Steps))
			for _, step := range workflow.Steps {
				actualNames = append(actualNames, step.Action.Name)
			}

			assert.DeepEqual(t, actualNames, tc.expected, "filtered action names should match expected")
		})
	}
}

func TestValidatePattern(t *testing.T) {
	validCases := []string{
		"*",
		"actions/*",
		"actions/checkout",
		"actions/setup-*",
		"custom/action",
		"github/*",
	}

	for _, pattern := range validCases {
		t.Run("valid/"+pattern, func(t *testing.T) {
			err := validatePattern(pattern)
			assert.NilError(t, err)
		})
	}

	invalidCases := []struct {
		pattern string
		wantErr string
	}{
		{"", "empty pattern not allowed"},
		{"*/*", "multiple wildcards not supported"},
		{"*/setup", "wildcards are only supported at the end of patterns"},
		{"act*/setup", "wildcards are only supported at the end of patterns"},
		{"actions/**", "multiple wildcards not supported"},
	}

	for _, tc := range invalidCases {
		t.Run("invalid/"+tc.pattern, func(t *testing.T) {
			err := validatePattern(tc.pattern)
			if err == nil {
				t.Fatal("expected error but got nil")
			}
			assert.Contains(t, err.Error(), tc.wantErr, "error message")
		})
	}
}

func TestActionRepo(t *testing.T) {
	testCases := []struct {
		name     string
		expected string
	}{
		{"actions/checkout", "actions/checkout"},
		{"slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml", "slsa-framework/slsa-github-generator"},
		{"owner/repo/path/to/action", "owner/repo"},
		{"owner/repo/.github/workflows/workflow.yaml", "owner/repo"},
		{"single-part", "single-part"},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			action := Action{Name: tc.name}
			got := action.Repo()
			assert.Equal(t, got, tc.expected, "incorrect repo extraction")
		})
	}
}
