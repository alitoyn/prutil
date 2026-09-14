package home_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/relloyd/prutil/internal/home"
	"github.com/relloyd/prutil/internal/model"
)

func TestTheSelfTestMarkerDefaultsToTheBuiltInOne(t *testing.T) {
	cfg, err := home.ParseConfig([]byte("watch:\n  base_interval: 5m\n"))

	require.NoError(t, err)
	assert.Equal(t, model.DefaultSelfTestMarker, cfg.Watch.Marker(),
		"a configuration that does not mention it keeps the built-in marker")
}

func TestTheSelfTestMarkerCanBeReplacedOrTurnedOff(t *testing.T) {
	// The empty string has to mean off rather than unset, which is why the
	// field is a pointer.
	replaced, err := home.ParseConfig([]byte("watch:\n  self_test_marker: \"<!-- mine -->\"\n"))
	require.NoError(t, err)
	assert.Equal(t, "<!-- mine -->", replaced.Watch.Marker())

	off, err := home.ParseConfig([]byte("watch:\n  self_test_marker: \"\"\n"))
	require.NoError(t, err)
	assert.Empty(t, off.Watch.Marker(), "an explicit empty marker turns the exception off")
}

func TestAZeroWatchConfigStillGetsTheBuiltInMarker(t *testing.T) {
	assert.Equal(t, model.DefaultSelfTestMarker, home.WatchConfig{}.Marker())
}

func TestTheWrittenTemplateNamesTheSelfTestMarker(t *testing.T) {
	assert.Contains(t, string(home.DefaultConfigTemplate()), "self_test_marker:")
}

func TestOneConfigurationCannotChangeAnothersDefaultMarker(t *testing.T) {
	// The YAML decoder writes through a non-nil pointer it finds in the
	// target, so a default pointing at shared memory would let one file
	// rewrite the built-in marker for every configuration parsed after it,
	// and two parsed at once would race over it.
	replaced, err := home.ParseConfig([]byte("watch:\n  self_test_marker: \"<!-- mine -->\"\n"))
	require.NoError(t, err)
	require.Equal(t, "<!-- mine -->", replaced.Watch.Marker())

	after, err := home.ParseConfig([]byte("watch:\n  base_interval: 5m\n"))
	require.NoError(t, err)
	assert.Equal(t, model.DefaultSelfTestMarker, after.Watch.Marker())
	assert.Equal(t, model.DefaultSelfTestMarker, home.DefaultConfig().Watch.Marker())
}

func TestReviewCommentDefaultsToBuiltIn(t *testing.T) {
	cfg, err := home.ParseConfig([]byte("watch:\n  base_interval: 5m\n"))
	require.NoError(t, err)
	assert.Equal(t, home.DefaultReviewComment, cfg.Review.CommentFor("relloyd/prutil"))
}

func TestReviewCommentCanBeOverridden(t *testing.T) {
	cfg, err := home.ParseConfig([]byte("review:\n  comment: \"/gemini review --full\"\n"))
	require.NoError(t, err)
	assert.Equal(t, "/gemini review --full", cfg.Review.CommentFor("relloyd/prutil"))
}

func TestReviewCommentEmptyStringDisablesFeature(t *testing.T) {
	cfg, err := home.ParseConfig([]byte("review:\n  comment: \"\"\n"))
	require.NoError(t, err)
	assert.Empty(t, cfg.Review.CommentFor("relloyd/prutil"))
}

func TestReviewCommentPerRepoOverrides(t *testing.T) {
	cfg, err := home.ParseConfig([]byte(`review:
  comment: "/gemini review"
  repos:
    org/coderabbit-repo: "@coderabbitai review"
    org/disabled-repo: ""
`))
	require.NoError(t, err)
	assert.Equal(t, "@coderabbitai review", cfg.Review.CommentFor("org/coderabbit-repo"))
	assert.Empty(t, cfg.Review.CommentFor("org/disabled-repo"))
	assert.Equal(t, "/gemini review", cfg.Review.CommentFor("org/other-repo"))
}

func TestTheWrittenTemplateNamesReviewComment(t *testing.T) {
	tmpl := string(home.DefaultConfigTemplate())
	assert.Contains(t, tmpl, "review:")
	assert.Contains(t, tmpl, "comment: \"/gemini review\"")
}

func TestBranchMatchAndFallbackDefaults(t *testing.T) {
	cfg := home.DefaultConfig()
	assert.Equal(t, home.BranchMatchFuzzy, cfg.Herdr.BranchMatch)
	assert.Equal(t, home.FallbackNew, cfg.Herdr.Fallback)
}

func TestBranchMatchCanBeConfigured(t *testing.T) {
	cases := []struct {
		input    string
		expected home.BranchMatchStrategy
	}{
		{"herdr:\n  branch_match: strict\n", home.BranchMatchStrict},
		{"herdr:\n  branch_match: exact\n", home.BranchMatchStrict},
		{"herdr:\n  branch_match: fuzzy\n", home.BranchMatchFuzzy},
		{"herdr:\n  branch_match: lenient\n", home.BranchMatchFuzzy},
	}
	for _, tc := range cases {
		cfg, err := home.ParseConfig([]byte(tc.input))
		require.NoError(t, err)
		assert.Equal(t, tc.expected, cfg.Herdr.BranchMatch)
	}
}

func TestFallbackStrategyCanBeConfigured(t *testing.T) {
	cases := []struct {
		input    string
		expected home.FallbackStrategy
	}{
		{"herdr:\n  fallback: new\n", home.FallbackNew},
		{"herdr:\n  fallback: provision\n", home.FallbackNew},
		{"herdr:\n  fallback: none\n", home.FallbackNone},
		{"herdr:\n  fallback: strict\n", home.FallbackNone},
		{"herdr:\n  fallback: repo\n", home.FallbackRepo},
		{"herdr:\n  fallback: repository\n", home.FallbackRepo},
	}
	for _, tc := range cases {
		cfg, err := home.ParseConfig([]byte(tc.input))
		require.NoError(t, err)
		assert.Equal(t, tc.expected, cfg.Herdr.Fallback)
	}
}

func TestInvalidBranchMatchOrFallbackReturnsError(t *testing.T) {
	_, err := home.ParseConfig([]byte("herdr:\n  branch_match: invalid\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch matching strategy")

	_, err = home.ParseConfig([]byte("herdr:\n  fallback: invalid\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback strategy")
}

func TestTheWrittenTemplateNamesBranchMatchAndFallback(t *testing.T) {
	tmpl := string(home.DefaultConfigTemplate())
	assert.Contains(t, tmpl, "branch_match: fuzzy")
	assert.Contains(t, tmpl, "fallback: new")
}
