package desktop

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder captures the program a notifier would have run, and answers which
// programs it should pretend are installed.
type recorder struct {
	env     []string
	name    string
	args    []string
	calls   int
	err     error
	present map[string]bool
}

func (r *recorder) run(_ context.Context, env []string, name string, args ...string) error {
	r.env, r.name, r.args = env, name, args
	r.calls++
	return r.err
}

func (r *recorder) look(name string) (string, error) {
	if r.present[name] {
		return "/usr/bin/" + name, nil
	}
	return "", exec.ErrNotFound
}

func system(goos string, rec *recorder) *System {
	return &System{GOOS: goos, Run: rec.run, Look: rec.look}
}

func TestMacOSPassesTheTextToOsascriptAsArguments(t *testing.T) {
	rec := &recorder{present: map[string]bool{"osascript": true}}
	title := `-e "x" & (do shell script "touch /tmp/pwned")`

	require.NoError(t, system("darwin", rec).Notify(Notification{Title: title, Body: "Add a retry"}))
	assert.Equal(t, "osascript", rec.name)
	require.GreaterOrEqual(t, len(rec.args), 4)
	assert.Equal(t, []string{"--", "prutil", title, "Add a retry"}, rec.args[len(rec.args)-4:],
		"the text follows --, so neither a leading dash nor a quote can become part of the script")
	for _, arg := range rec.args[:len(rec.args)-4] {
		assert.NotContains(t, arg, "pwned", "the script itself never contains the text")
	}
}

func TestLinuxEscapesMarkupInTheBodyOnly(t *testing.T) {
	rec := &recorder{present: map[string]bool{"notify-send": true}}

	require.NoError(t, system("linux", rec).Notify(Notification{Title: "a <b> title", Body: "<b>bold</b> & more"}))
	assert.Equal(t, "notify-send", rec.name)
	assert.Equal(t, []string{"--app-name=prutil", "--", "a <b> title", "&lt;b&gt;bold&lt;/b&gt; &amp; more"}, rec.args)
}

func TestWindowsPassesTheTextThroughTheEnvironment(t *testing.T) {
	rec := &recorder{present: map[string]bool{"powershell.exe": true}}
	title := `'; Remove-Item -Recurse C:\ #`

	require.NoError(t, system("windows", rec).Notify(Notification{Title: title, Body: "Add a retry"}))
	assert.Equal(t, "powershell.exe", rec.name)
	assert.Equal(t, []string{"PRUTIL_TOAST_TITLE=prutil: " + title, "PRUTIL_TOAST_BODY=Add a retry"}, rec.env)
	for _, arg := range rec.args {
		assert.NotContains(t, arg, "Remove-Item", "the script itself never contains the text")
	}
	assert.NotContains(t, windowsToast, "\n", "the script travels as one argument")
}

func TestAMissingProgramIsNamed(t *testing.T) {
	cases := []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: "osascript"},
		{goos: "windows", want: "powershell.exe"},
		{goos: "linux", want: "notify-send, which is not installed (it comes with libnotify)"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			rec := &recorder{}
			s := system(tc.goos, rec)

			err := s.Available()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)

			err = s.Notify(Notification{Title: "approved"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Zero(t, rec.calls, "nothing is run when the program is not there")
		})
	}
}

func TestAvailableIsQuietWhenTheProgramIsThere(t *testing.T) {
	rec := &recorder{present: map[string]bool{"osascript": true}}
	assert.NoError(t, system("darwin", rec).Available())
	assert.Zero(t, rec.calls, "asking shows nothing")
}

func TestAFailedProgramIsReported(t *testing.T) {
	rec := &recorder{present: map[string]bool{"notify-send": true}, err: errors.New("no notification server")}

	err := system("linux", rec).Notify(Notification{Title: "approved"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notify-send")
	assert.Contains(t, err.Error(), "no notification server")
}

func TestANotificationNeedsATitle(t *testing.T) {
	rec := &recorder{present: map[string]bool{"notify-send": true}}
	assert.Error(t, system("linux", rec).Notify(Notification{Title: " \u202e\x1b ", Body: "body"}))
	assert.Zero(t, rec.calls)
}

func TestTheTextIsCleanedBeforeItIsShown(t *testing.T) {
	rec := &recorder{present: map[string]bool{"notify-send": true}}

	title := "relloyd/prutil#42\x1b[31m\u202e approved\r\n\tnow"
	body := strings.Repeat("word ", 100)
	require.NoError(t, system("linux", rec).Notify(Notification{Title: title, Body: body}))

	assert.Equal(t, "relloyd/prutil#42[31m approved now", rec.args[2],
		"escape, bidi override and line breaks are gone; the visible text is kept")
	shown := []rune(rec.args[3])
	assert.Len(t, shown, maxBody)
	assert.Equal(t, '…', shown[len(shown)-1], "a cut body says so")
}

func TestClean(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{name: "plain text is untouched", text: "Add a retry", limit: 20, want: "Add a retry"},
		{name: "whitespace runs collapse and the ends are trimmed", text: "  a \n\n b\t", limit: 20, want: "a b"},
		{name: "controls vanish rather than becoming spaces", text: "a\x00b\x7fc\u0085d", limit: 20, want: "abc d"},
		{name: "zero-width and bidi characters vanish", text: "a\u200bb\u2066c\ufeff", limit: 20, want: "abc"},
		{name: "text at the limit is kept whole", text: "abcde", limit: 5, want: "abcde"},
		{name: "longer text is cut to the limit, ellipsis included", text: "abcdef", limit: 5, want: "abcd…"},
		{name: "a cut never leaves a space before the ellipsis", text: "abc def", limit: 5, want: "abc…"},
		{name: "characters are counted rather than bytes", text: "ééééé", limit: 5, want: "ééééé"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, clean(tc.text, tc.limit))
		})
	}
}
