package ui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/relloyd/prutil/internal/home"
)

// openSettingsPane presses s and checks the pane came up.
func openSettingsPane(t *testing.T, app *App) {
	t.Helper()
	send(t, app, press("s"))
	require.True(t, app.settings.open, "s opens the settings")
}

// approvedRow is the settings pane's row for approval notifications, as drawn.
func approvedRow(t *testing.T, app *App) string {
	t.Helper()
	for _, line := range lines(app) {
		if strings.Contains(line, "Pull request approved") {
			return line
		}
	}
	require.Fail(t, "the settings pane shows no approval row")
	return ""
}

func TestEveryNotificationHasOneEntryInTheSettings(t *testing.T) {
	events := make([]home.NotificationEvent, 0, len(notifications))
	for _, n := range notifications {
		assert.NotEmpty(t, n.setting)
		assert.NotEmpty(t, n.detail)
		assert.NotEmpty(t, n.headline)
		assert.NotNil(t, n.fired)
		events = append(events, n.event)
	}
	assert.Equal(t, home.NotificationEvents(), events,
		"the settings list every notification home knows about, once and in the same order")
}

func TestSOpensTheSettingsAndEscClosesThem(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	focus, cursor := app.focus, app.cur().cursor

	openSettingsPane(t, app)
	screen := plain(app.render())
	assert.Contains(t, screen, "Settings")
	assert.Contains(t, screen, "DESKTOP NOTIFICATIONS")
	row := approvedRow(t, app)
	assert.Contains(t, row, "[✓]")
	assert.Contains(t, row, "on")
	assert.Contains(t, screen, "space toggle")

	send(t, app, press("esc"))
	assert.False(t, app.settings.open)
	assert.Equal(t, focus, app.focus, "esc closes the pane rather than going back a pane")
	assert.Equal(t, cursor, app.cur().cursor)
	assert.NotContains(t, plain(app.render()), "DESKTOP NOTIFICATIONS")
}

func TestTheKeysThatOpenTheSettingsAlsoCloseThem(t *testing.T) {
	for _, k := range []string{"s", ",", "q"} {
		t.Run(k, func(t *testing.T) {
			app, _, _ := newTestApp(t, 120, 40)
			send(t, app, press(","))
			require.True(t, app.settings.open, ", opens the settings too")

			msgs := drain(send(t, app, press(k)))
			assert.False(t, app.settings.open)
			assert.NotContains(t, msgs, tea.QuitMsg{}, "q closes the pane rather than quitting")
		})
	}
}

func TestCtrlCStillQuitsFromTheSettings(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)
	assert.Contains(t, drain(send(t, app, press("ctrl+c"))), tea.QuitMsg{})
}

func TestKeysDoNotReachTheAppWhileTheSettingsAreOpen(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	gen, active, cursor, focus := app.gen, app.active, app.cur().cursor, app.focus
	openSettingsPane(t, app)

	for _, k := range []string{"r", "j", "G", "a", "w", "l", "tab", "right", "?", "W", "y"} {
		send(t, app, press(k))
	}
	send(t, app, tea.PasteMsg{Content: "rjw"})

	assert.True(t, app.settings.open)
	assert.False(t, app.overlay.open, "? does not open the shortcuts over the settings")
	assert.Equal(t, gen, app.gen, "r did not refresh")
	assert.Equal(t, active, app.active, "tab did not switch the view")
	assert.Equal(t, cursor, app.cur().cursor, "j and G did not move the list")
	assert.Equal(t, focus, app.focus, "l and → did not focus the detail pane")
	assert.Zero(t, app.autoLeft, "a did not start auto-refresh")
	assert.False(t, app.armed(app.selectedKey()), "w did not watch anything")
	assert.Empty(t, clipboardOf(t, app).copied(), "y did not copy")
}

func TestTheShortcutOverlayOpensTheSettings(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openOverlay(t, app, "settings")
	assert.Equal(t, "settings", firstTitle(t, app))

	send(t, app, press("enter"))
	assert.False(t, app.overlay.open)
	assert.True(t, app.settings.open, "enter on settings opens them")
}

func TestSpaceTurnsANotificationOffAndSavesIt(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)

	send(t, app, press("space"))
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved))
	row := approvedRow(t, app)
	assert.Contains(t, row, "[ ]")
	assert.Contains(t, row, "off")
	assert.Contains(t, plain(app.render()), "Pull request approved is off · saved")

	saved, err := app.store.LoadConfig()
	require.NoError(t, err)
	assert.False(t, saved.Notifications.Enabled(home.NotifyApproved), "the next run starts with it off")

	send(t, app, press("enter"))
	assert.True(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "enter toggles as well")
	send(t, app, press("x"))
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "and so does x")
	saved, err = app.store.LoadConfig()
	require.NoError(t, err)
	assert.False(t, saved.Notifications.Enabled(home.NotifyApproved))
}

func TestTheSettingsDoNotWriteIntoTheCallersConfiguration(t *testing.T) {
	cfg := fastWatch()
	app := New(Config{Client: newFakeClient(nil, nil), Home: cfg, Store: home.OpenIn(t.TempDir()), Notifier: &fakeNotifier{}})
	send(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	openSettingsPane(t, app)
	send(t, app, press("space"))

	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved))
	assert.True(t, cfg.Notifications.Enabled(home.NotifyApproved), "the configuration main loaded is not the app's to change")
}

func TestASettingThatCannotBeSavedStillAppliesAndSaysSo(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	require.NoError(t, os.WriteFile(app.store.Path(home.ConfigFile), []byte("herdr: [\n"), 0o600))
	openSettingsPane(t, app)

	send(t, app, press("space"))
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "the change applies for this run")
	assert.True(t, app.settings.noticeErr)
	assert.Contains(t, app.settings.notice, "is off until prutil quits, but was not saved")
	assert.Contains(t, app.settings.notice, "could not read the configuration")

	data, err := os.ReadFile(app.store.Path(home.ConfigFile))
	require.NoError(t, err)
	assert.Equal(t, "herdr: [\n", string(data), "the file that does not parse is left as it was")
}

func TestWithoutAnApplicationDirectoryASettingLastsTheSession(t *testing.T) {
	app := New(Config{
		Client:   newFakeClient(samplePRs(), sampleChecks()),
		Home:     fastWatch(),
		StoreErr: errors.New("no home directory"),
		Notifier: &fakeNotifier{},
	})
	send(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	openSettingsPane(t, app)
	assert.Contains(t, plain(app.render()), "not saved", "the frame says there is nowhere to save to")

	send(t, app, press("space"))
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved))
	assert.Contains(t, app.settings.notice, "not saved: no home directory")
}

func TestTheSettingsSayWhenNothingWouldAppear(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	notifierOf(t, app).unavailable = errors.New("desktop notifications need notify-send, which is not installed")

	openSettingsPane(t, app)
	assert.Contains(t, plain(app.render()), "need notify-send")

	send(t, app, press("space"))
	send(t, app, press("space"))
	assert.True(t, app.settings.noticeErr, "turning it on says it will not help")
	assert.Contains(t, app.settings.notice, "is on and saved, but nothing will appear: desktop notifications need notify-send")
}

func TestWithoutANotifierTheSettingsSayNotificationsCannotBeShown(t *testing.T) {
	app := New(Config{Client: newFakeClient(samplePRs(), nil), Home: fastWatch(), Store: home.OpenIn(t.TempDir())})
	send(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	openSettingsPane(t, app)

	assert.Contains(t, plain(app.render()), "prutil cannot show desktop notifications here")
	assert.Nil(t, send(t, app, press("t")))
	assert.True(t, app.settings.noticeErr)
}

func TestTSendsATestNotification(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	notifierOf(t, app).hint = "Allow notifications from Script Editor."
	openSettingsPane(t, app)

	cmd := send(t, app, press("t"))
	assert.Contains(t, app.settings.notice, "sending a test notification")
	for _, msg := range drain(cmd) {
		send(t, app, msg)
	}

	shown := notifierOf(t, app).notifications()
	require.Len(t, shown, 1)
	assert.Equal(t, "Test notification", shown[0].Title)
	assert.False(t, app.settings.noticeErr)
	assert.Equal(t, "sent a test notification. Nothing appeared? Allow notifications from Script Editor.", app.settings.notice)
	assert.Contains(t, plain(app.render()), "Nothing appeared?")
}

func TestAFailedTestNotificationIsReported(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	notifierOf(t, app).err = errors.New("osascript: not allowed")
	openSettingsPane(t, app)

	for _, msg := range drain(send(t, app, press("t"))) {
		send(t, app, msg)
	}
	assert.True(t, app.settings.noticeErr)
	assert.Equal(t, "the test notification failed: osascript: not allowed", app.settings.notice)
}

func TestATestNotificationThatFinishesAfterThePaneClosedGoesToTheStatusLine(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)
	cmd := send(t, app, press("t"))
	send(t, app, press("esc"))

	var reply tea.Cmd
	for _, msg := range drain(cmd) {
		reply = send(t, app, msg)
	}
	require.NotNil(t, reply)
	assert.Equal(t, statusMsg("sent a test notification"), reply())
}

func TestClickingASettingTogglesItAndClicksElsewhereDoNothing(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	cursor := app.cur().cursor
	openSettingsPane(t, app)
	l := app.settingsLayout()

	send(t, app, click(0, headerHeight+rowHeight))
	assert.True(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "a click outside the pane changes nothing")
	assert.Equal(t, cursor, app.cur().cursor, "and does not reach the list behind it")
	assert.True(t, app.settings.open)

	send(t, app, click(l.x+4, l.y+1))
	assert.True(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "a click on the heading changes nothing")

	send(t, app, click(l.x+4, l.y+2))
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "a click on the row toggles it")
	send(t, app, tea.MouseClickMsg{X: l.x + 4, Y: l.y + 2, Button: tea.MouseRight})
	assert.False(t, app.homeCfg.Notifications.Enabled(home.NotifyApproved), "only the left button toggles")

	send(t, app, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	assert.Equal(t, cursor, app.cur().cursor, "the wheel does not scroll the list behind the pane")
}

func TestMovingTheSelectionStaysInsideTheListAndClearsTheNotice(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)
	send(t, app, press("space"))
	require.NotEmpty(t, app.settings.notice)

	send(t, app, press("k"))
	assert.Zero(t, app.settings.cursor, "up at the top stays put")
	assert.NotEmpty(t, app.settings.notice, "staying put keeps the notice")
	send(t, app, press("j"))
	assert.Equal(t, 1, app.settings.cursor)
	assert.Empty(t, app.settings.notice, "a notice about another row is cleared")
}

func TestTheSettingsExplainTheSelectedNotificationAndHowOftenPrutilLooks(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	app.homeCfg.Notifications.Interval = home.Duration(2 * time.Minute)
	openSettingsPane(t, app)

	screen := plain(app.render())
	assert.Contains(t, screen, "When one of your open pull requests is approved")
	assert.Contains(t, screen, "every 2 minutes")
}

func TestTheSettingsReportAFailedCheckForChanges(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	send(t, app, notifyPollMsg{err: errors.New("could not resolve host")})
	openSettingsPane(t, app)

	assert.Contains(t, plain(app.render()), "the last check for changes failed: could not resolve host")
}

func TestHumanInterval(t *testing.T) {
	cases := map[string]string{
		"1m0s":  "minute",
		"2m0s":  "2 minutes",
		"30s":   "30 seconds",
		"1m30s": "1m30s",
	}
	for in, want := range cases {
		d, err := time.ParseDuration(in)
		require.NoError(t, err)
		assert.Equal(t, want, humanInterval(d), in)
	}
}

func TestTheSettingsFitEveryTerminalSize(t *testing.T) {
	sizes := []struct{ width, height int }{
		{40, 12}, {60, 20}, {79, 24}, {80, 24}, {100, 30}, {200, 60}, {120, 8}, {45, 30}, {20, 6},
	}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			app, _, _ := newTestApp(t, size.width, size.height)
			notifierOf(t, app).unavailable = errors.New("desktop notifications need notify-send, which is not installed (it comes with libnotify)")
			openSettingsPane(t, app)

			for step := 0; step < 2; step++ {
				rendered := lines(app)
				assert.Len(t, rendered, size.height, "the pane keeps the screen exactly the terminal's height")
				for i, line := range rendered {
					assert.LessOrEqual(t, ansi.StringWidth(line), size.width,
						"line %d overflows the terminal: %q", i, line)
				}
				assert.True(t, slices.ContainsFunc(rendered, func(line string) bool {
					return strings.Contains(line, "[✓]") || strings.Contains(line, "[ ]")
				}), "the setting itself is always on screen")
				send(t, app, press("space"))
			}
		})
	}
}

func TestSettingsSteppingAndCycling(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	app.homeCfg.Notifications.Interval = home.Duration(2 * time.Minute)
	openSettingsPane(t, app)

	// Navigate to Check poll interval (item 1)
	send(t, app, press("j"))
	assert.Equal(t, 1, app.settings.cursor)

	// Step interval up with +
	send(t, app, press("+"))
	assert.Equal(t, home.Duration(2*time.Minute+30*time.Second), app.homeCfg.Notifications.Interval)
	assert.Contains(t, app.settings.notice, "Check poll interval set to 2m30s · saved")

	// Step interval down with -
	send(t, app, press("-"))
	assert.Equal(t, home.Duration(2*time.Minute), app.homeCfg.Notifications.Interval)

	// Jump to next section with tab (WATCHING & POLLING)
	send(t, app, press("tab"))
	assert.Equal(t, 2, app.settings.cursor) // watch.active_interval

	// Jump to next section with tab (AI REVIEW TRIGGER)
	send(t, app, press("tab"))
	assert.Equal(t, 11, app.settings.cursor) // review.comment

	// Jump to next section with tab (CODING AGENT)
	send(t, app, press("tab"))
	assert.Equal(t, 13, app.settings.cursor) // herdr.fallback

	// Cycle fallback strategy
	assert.Equal(t, home.FallbackNew, app.homeCfg.Herdr.Fallback)
	send(t, app, press("right"))
	assert.Equal(t, home.FallbackNone, app.homeCfg.Herdr.Fallback)
	assert.Contains(t, app.settings.notice, "Fallback strategy set to \"none\" · saved")

	send(t, app, press("right"))
	assert.Equal(t, home.FallbackRepo, app.homeCfg.Herdr.Fallback)

	send(t, app, press("d")) // Reset to default
	assert.Equal(t, home.FallbackNew, app.homeCfg.Herdr.Fallback)
	assert.Contains(t, app.settings.notice, "reset to default · saved")
}

func TestSettingsInlineTextEditing(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)

	// Jump to review.comment (item 11)
	send(t, app, press("tab"))
	send(t, app, press("tab"))
	assert.Equal(t, 11, app.settings.cursor)

	// Press enter to edit
	send(t, app, press("enter"))
	assert.Equal(t, settingsModeEdit, app.settings.mode)

	// Clear and enter new comment
	app.settings.input.SetValue("/claude review")
	send(t, app, press("enter"))
	assert.Equal(t, settingsModeNormal, app.settings.mode)
	assert.Equal(t, "/claude review", app.homeCfg.Review.CommentFor(""))
	assert.Contains(t, app.settings.notice, "Review comment set to \"/claude review\" · saved")
}

func TestSettingsSubPaneMapAndSequence(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	openSettingsPane(t, app)

	// Jump to last section (REPOSITORIES & DISCOVERY)
	send(t, app, press("G"))
	assert.Equal(t, len(allSettings())-1, app.settings.cursor) // discovery.roots

	// Open sub-pane
	send(t, app, press("enter"))
	assert.Equal(t, settingsModeSubPane, app.settings.mode)
	assert.Equal(t, subPaneDiscoveryRoots, app.settings.subPane.kind)

	// Add a root
	send(t, app, press("a"))
	assert.True(t, app.settings.subPane.adding)
	app.settings.subPane.valInput.SetValue("~/src")
	send(t, app, press("enter"))
	assert.False(t, app.settings.subPane.adding)
	assert.Equal(t, []string{"~/src"}, app.homeCfg.Discovery.Roots)
	assert.Contains(t, app.settings.notice, "Added discovery root \"~/src\" · saved")

	// Delete the root
	send(t, app, press("d"))
	assert.Empty(t, app.homeCfg.Discovery.Roots)
	assert.Contains(t, app.settings.notice, "Deleted discovery root \"~/src\" · saved")

	// Close sub-pane with esc
	send(t, app, press("esc"))
	assert.Equal(t, settingsModeNormal, app.settings.mode)
}
