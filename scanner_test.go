package main

import (
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
			line: "uses: owner/repo@v1.2.3@foo # malformed ref",
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
