package home

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var approvedPath = []string{"notifications", "events", "approved"}

func TestSetScalarChangesOnlyTheOneValue(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a plain value is replaced and its comment kept",
			src: "# mine\nherdr:\n  skill: triage   # the skill\n\nnotifications:\n" +
				"  interval: 5m\n  events:\n    approved: true    # on approval\n\nrepos: {}\n",
			want: "# mine\nherdr:\n  skill: triage   # the skill\n\nnotifications:\n" +
				"  interval: 5m\n  events:\n    approved: false    # on approval\n\nrepos: {}\n",
		},
		{
			name: "a quoted value is replaced quotes and all",
			src:  "notifications:\n  events:\n    approved: \"yes\" # quoted\n",
			want: "notifications:\n  events:\n    approved: false # quoted\n",
		},
		{
			name: "a single-quoted value with an escaped quote is replaced whole",
			src:  "notifications:\n  events:\n    approved: 'it''s' # quoted\n",
			want: "notifications:\n  events:\n    approved: false # quoted\n",
		},
		{
			name: "a key with no value is given one",
			src:  "notifications:\n  events:\n    approved:\n  interval: 5m\n",
			want: "notifications:\n  events:\n    approved: false\n  interval: 5m\n",
		},
		{
			name: "a missing event is added beneath its siblings",
			src:  "notifications:\n  events:\n    merged: true\n  interval: 5m\nrepos: {}\n",
			want: "notifications:\n  events:\n    merged: true\n    approved: false\n  interval: 5m\nrepos: {}\n",
		},
		{
			name: "a missing events mapping is added inside notifications",
			src:  "notifications:\n    interval: 5m\n\nrepos: {}\n",
			want: "notifications:\n    interval: 5m\n    events:\n        approved: false\n\nrepos: {}\n",
		},
		{
			name: "a missing section is appended after a blank line",
			src:  "herdr:\n  skill: triage\n",
			want: "herdr:\n  skill: triage\n\nnotifications:\n  events:\n    approved: false\n",
		},
		{
			name: "a file without a final newline gains one with the new section",
			src:  "herdr:\n  skill: triage",
			want: "herdr:\n  skill: triage\n\nnotifications:\n  events:\n    approved: false\n",
		},
		{
			name: "an empty file gains the section",
			src:  "",
			want: "notifications:\n  events:\n    approved: false\n",
		},
		{
			name: "a file of nothing but comments keeps them",
			src:  "# nothing yet\n",
			want: "# nothing yet\n\nnotifications:\n  events:\n    approved: false\n",
		},
		{
			name: "an empty section is filled in",
			src:  "notifications: # later\nrepos: {}\n",
			want: "notifications: # later\n  events:\n    approved: false\nrepos: {}\n",
		},
		{
			name: "a null section loses its tilde and is filled in",
			src:  "notifications: ~\nrepos: {}\n",
			want: "notifications:\n  events:\n    approved: false\nrepos: {}\n",
		},
		{
			name: "an empty flow mapping is removed and filled in",
			src:  "notifications:\n  events: {}  # none yet\n",
			want: "notifications:\n  events:  # none yet\n    approved: false\n",
		},
		{
			name: "a block scalar sibling sends the new key above it rather than into it",
			src:  "notifications:\n  note: |\n    text\n\n    more\n  interval: 5m\n",
			want: "notifications:\n  events:\n    approved: false\n  note: |\n    text\n\n    more\n  interval: 5m\n",
		},
		{
			name: "CRLF line endings are kept, and copied onto new lines",
			src:  "notifications:\r\n  events:\r\n    merged: true\r\n",
			want: "notifications:\r\n  events:\r\n    merged: true\r\n    approved: false\r\n",
		},
		{
			name: "a quoted value holding wide characters is replaced whole",
			src:  "notifications:\n  events:\n    approved: \"✓ oui\" # ü\n",
			want: "notifications:\n  events:\n    approved: false # ü\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := setScalar([]byte(tc.src), approvedPath, "false")
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))

			cfg, err := ParseConfig(got)
			require.NoError(t, err)
			assert.False(t, cfg.Notifications.Enabled(NotifyApproved))
		})
	}
}

func TestTheParsersColumnsAreCountedInCharactersNotBytes(t *testing.T) {
	text := newYAMLText([]byte("é✓: x\n"))

	line, at, err := text.offset(1, 5)
	require.NoError(t, err)
	assert.Zero(t, line)
	assert.Equal(t, "x", text.lines[0][at:], "the fifth character starts after five bytes of the first two")

	_, _, err = text.offset(1, 7)
	assert.Error(t, err, "a column past the end of the line is not taken on trust")
	_, _, err = text.offset(2, 1)
	assert.Error(t, err, "nor is a line past the end of the file")
}

func TestSetScalarRefusesWhatItCannotEditSafely(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "a file that does not parse", src: "notifications: [\n"},
		{name: "a document that is not a mapping", src: "- one\n- two\n"},
		{name: "a document written as one flow mapping", src: "{notifications: {}}\n"},
		{name: "a section written in flow style", src: "notifications: {events: {approved: true}}\n"},
		{name: "a section that is a list", src: "notifications:\n  - approved\n"},
		{name: "a value written as a block scalar", src: "notifications:\n  events:\n    approved: |\n      true\n"},
		{name: "a value that carries on over two lines", src: "notifications:\n  events:\n    approved: tr\n      ue\n"},
		{name: "a value that is a mapping", src: "notifications:\n  events:\n    approved:\n      on: true\n"},
		{name: "a value with an explicit tag", src: "notifications:\n  events:\n    approved: !!bool true\n"},
		{name: "a value borrowed through an alias", src: "on: &yes true\nnotifications:\n  events:\n    approved: *yes\n"},
		{name: "a section shared through an anchor", src: "notifications: &n\n  events:\n    approved: true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setScalar([]byte(tc.src), approvedPath, "false")
			assert.Error(t, err)
		})
	}
}

func TestSetScalarRefusesKeysItWouldHaveToQuote(t *testing.T) {
	_, err := setScalar([]byte(""), []string{"notifications", "a: b"}, "true")
	assert.Error(t, err)
	_, err = setScalar([]byte(""), nil, "true")
	assert.Error(t, err)
}

func TestSetScalarLeavesTheTemplateAsItWasApartFromTheOneValue(t *testing.T) {
	src := DefaultConfigTemplate()
	got, err := setScalar(src, approvedPath, "false")
	require.NoError(t, err)

	want := string(src)
	// The template writes each event on a line of its own, so exactly one
	// line may differ.
	assert.Equal(t, 1, countDiffLines(want, string(got)))
	assert.Contains(t, string(got), "    approved: false\n")
}

// countDiffLines counts the lines at which two texts of equal line count
// differ, or -1 when the counts differ.
func countDiffLines(a, b string) int {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(la) != len(lb) {
		return -1
	}
	n := 0
	for i := range la {
		if la[i] != lb[i] {
			n++
		}
	}
	return n
}

func TestSetBlockScalarReplacesMultilineBlockScalar(t *testing.T) {
	src := "herdr:\n  prompt: |-\n    old line 1\n    old line 2\n  skill: triage\n"
	got, err := setBlockScalar([]byte(src), []string{"herdr", "prompt"}, "new line 1\nnew line 2")
	require.NoError(t, err)

	cfg, err := ParseConfig(got)
	require.NoError(t, err)
	assert.Equal(t, "new line 1\nnew line 2", cfg.Herdr.Prompt)
	assert.Equal(t, "triage", cfg.Herdr.Skill)
	assert.Contains(t, string(got), "  prompt: |-\n    new line 1\n    new line 2\n")
}

func TestSetMapEntryAddsAndUpdatesMappingEntries(t *testing.T) {
	src := "repos: {}\nreview:\n  comment: /gemini review\n"
	got, err := setMapEntry([]byte(src), []string{"repos"}, "owner/repo", "~/src/repo")
	require.NoError(t, err)

	cfg, err := ParseConfig(got)
	require.NoError(t, err)
	assert.Equal(t, "~/src/repo", cfg.Repos["owner/repo"])

	// Update existing
	got2, err := setMapEntry(got, []string{"repos"}, "owner/repo", "~/work/repo")
	require.NoError(t, err)
	cfg2, err := ParseConfig(got2)
	require.NoError(t, err)
	assert.Equal(t, "~/work/repo", cfg2.Repos["owner/repo"])

	// Delete entry
	got3, err := deleteMapEntry(got2, []string{"repos"}, "owner/repo")
	require.NoError(t, err)
	cfg3, err := ParseConfig(got3)
	require.NoError(t, err)
	assert.Empty(t, cfg3.Repos["owner/repo"])
}

func TestSetSequenceUpdatesList(t *testing.T) {
	src := "discovery:\n  roots: []\n"
	got, err := setSequence([]byte(src), []string{"discovery", "roots"}, []string{"~/src", "~/work"})
	require.NoError(t, err)

	cfg, err := ParseConfig(got)
	require.NoError(t, err)
	assert.Equal(t, []string{"~/src", "~/work"}, cfg.Discovery.Roots)
}

func TestDeleteKeyRemovesKeyFromYAML(t *testing.T) {
	src := "watch:\n  active_interval: 15s\n  base_interval: 2m\n"
	got, err := deleteKey([]byte(src), []string{"watch", "active_interval"})
	require.NoError(t, err)

	cfg, err := ParseConfig(got)
	require.NoError(t, err)
	assert.Equal(t, DefaultConfig().Watch.ActiveInterval, cfg.Watch.ActiveInterval)
	assert.NotContains(t, string(got), "active_interval")
	assert.Contains(t, string(got), "base_interval: 2m")
}
