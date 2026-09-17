// Package desktop raises the operating system's own notifications. It is a
// package of its own, like browser and clipboard, so the TUI can be tested
// against a recording notifier rather than putting toasts on the screen of
// whoever runs the tests.
package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// Notification is one toast: a short headline and a line beneath it.
type Notification struct {
	Title string
	Body  string
}

// Notifier shows desktop notifications.
type Notifier interface {
	// Notify shows one notification.
	Notify(n Notification) error
	// Available says why nothing would be shown, or nil when something would.
	// It is asked before anything needs showing, so the settings pane can say
	// that turning a notification on will not help.
	Available() error
}

// appName is how every notification is attributed. macOS attributes a toast
// from osascript to Script Editor and Windows to PowerShell, so on those the
// name goes into the text; notify-send is told it directly.
const appName = "prutil"

// notifyTimeout bounds one notification. The programs involved return as soon
// as the toast is queued, so anything this slow has hung.
const notifyTimeout = 5 * time.Second

// Limits on what a notification carries. The text comes from GitHub, where
// anybody who can open a pull request chooses its title.
const (
	maxTitle = 80
	maxBody  = 200
)

// runFunc runs a program to completion. env is added to the program's
// environment. It is a field so tests can intercept it.
type runFunc func(ctx context.Context, env []string, name string, args ...string) error

// lookFunc reports whether a program is on PATH. It is a field for the same
// reason.
type lookFunc func(name string) (string, error)

// System shows notifications with whatever the platform provides.
type System struct {
	// GOOS overrides the detected operating system. Empty means runtime.GOOS.
	GOOS string
	// Run runs the chosen program. Empty means start a real process.
	Run runFunc
	// Look resolves a program on PATH. Empty means exec.LookPath.
	Look lookFunc
}

// New returns a System configured for the current platform.
func New() *System {
	return &System{GOOS: runtime.GOOS}
}

// command is one program and how to call it.
type command struct {
	name string
	args []string
	env  []string
}

// Available implements Notifier.
func (s *System) Available() error {
	_, err := s.command(Notification{})
	return err
}

// Notify implements Notifier.
func (s *System) Notify(n Notification) error {
	n = Notification{Title: clean(n.Title, maxTitle), Body: clean(n.Body, maxBody)}
	if n.Title == "" {
		return fmt.Errorf("a notification needs a title")
	}

	cmd, err := s.command(n)
	if err != nil {
		return err
	}
	run := s.Run
	if run == nil {
		run = runProcess
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	if err := run(ctx, cmd.env, cmd.name, cmd.args...); err != nil {
		return fmt.Errorf("showing a notification with %s: %w", cmd.name, err)
	}
	return nil
}

// command picks the program for this platform and builds its arguments.
//
// The text only ever travels as an argument or an environment variable, never
// as part of a script, so a title written to look like AppleScript or
// PowerShell is shown rather than run.
func (s *System) command(n Notification) (command, error) {
	look := s.Look
	if look == nil {
		look = exec.LookPath
	}

	switch s.goos() {
	case "darwin":
		// The run handler takes the text as arguments. The -- keeps a title
		// that starts with a dash from being read as one of osascript's own
		// options.
		cmd := command{name: "osascript", args: []string{
			"-e", "on run argv",
			"-e", "display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)",
			"-e", "end run",
			"--", appName, n.Title, n.Body,
		}}
		return cmd, s.need(look, cmd.name)

	case "windows":
		// Windows PowerShell, not pwsh: only the former can reach the toast
		// API without a module. The text arrives through the environment and
		// becomes XML text nodes, which escape it.
		cmd := command{
			name: "powershell.exe",
			args: []string{"-NoProfile", "-NonInteractive", "-Command", windowsToast},
			env: []string{
				"PRUTIL_TOAST_TITLE=" + appName + ": " + n.Title,
				"PRUTIL_TOAST_BODY=" + n.Body,
			},
		}
		return cmd, s.need(look, cmd.name)

	default:
		// Notification servers may read the body as markup, so the three
		// characters that would start some are escaped. The summary is always
		// plain text.
		cmd := command{name: "notify-send", args: []string{
			"--app-name=" + appName, "--", n.Title, markup.Replace(n.Body),
		}}
		return cmd, s.need(look, cmd.name)
	}
}

// Hint says where to look when a notification was shown without error and
// still did not appear, which is a setting of the operating system's.
func (s *System) Hint() string {
	switch s.goos() {
	case "darwin":
		// osascript's notifications are Script Editor's as far as macOS is
		// concerned, which is not a name anybody would think to look for.
		return "Allow notifications from Script Editor in System Settings › Notifications."
	case "windows":
		return "Allow notifications from Windows PowerShell in Settings › System › Notifications."
	default:
		return "Check that a notification server is running and that Do Not Disturb is off."
	}
}

// need says what is missing when a program is not installed, because the fix
// is to install it rather than anything to do with prutil.
func (s *System) need(look lookFunc, name string) error {
	if _, err := look(name); err != nil {
		if name == "notify-send" {
			return fmt.Errorf("desktop notifications need notify-send, which is not installed (it comes with libnotify)")
		}
		return fmt.Errorf("desktop notifications need %s, which was not found", name)
	}
	return nil
}

func (s *System) goos() string {
	if s.GOOS == "" {
		return runtime.GOOS
	}
	return s.GOOS
}

// markup escapes the characters a notification server would read as markup.
var markup = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// windowsToast shows a toast attributed to Windows PowerShell, which is
// registered to show them, unlike an application id prutil would invent. It is
// one line, statements separated, because it travels as a single argument.
var windowsToast = strings.Join([]string{
	"$ErrorActionPreference = 'Stop'",
	"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null",
	"$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)",
	"$text = $xml.GetElementsByTagName('text')",
	"$text.Item(0).AppendChild($xml.CreateTextNode($env:PRUTIL_TOAST_TITLE)) | Out-Null",
	"$text.Item(1).AppendChild($xml.CreateTextNode($env:PRUTIL_TOAST_BODY)) | Out-Null",
	"$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\\WindowsPowerShell\\v1.0\\powershell.exe'",
	"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($xml))",
}, "; ")

// clean makes text from GitHub fit to show: control and invisible formatting
// characters removed, runs of whitespace, newlines included, collapsed to one
// space, and the result cut to limit characters.
//
// The formatting characters matter as much as the controls. A right-to-left
// override makes a title read differently from what it says, and a
// notification is exactly where a reader will not look twice.
func clean(text string, limit int) string {
	kept := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, text)
	runes := []rune(strings.Join(strings.Fields(kept), " "))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimRight(string(runes[:limit-1]), " ") + "…"
}

// runProcess runs the program and waits for it, so a failure can be reported.
func runProcess(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if text := strings.TrimSpace(string(out)); text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}
