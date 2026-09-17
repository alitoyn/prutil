package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/relloyd/prutil/internal/desktop"
	"github.com/relloyd/prutil/internal/home"
	"github.com/relloyd/prutil/internal/model"
)

const (
	// settingsMaxWidth keeps the pane about as wide as its longest sentence.
	settingsMaxWidth = 72
	// settingsDetailLines is how much of the selected setting's explanation is
	// shown beneath the list.
	settingsDetailLines = 3
	// settingsNoticeLines is the room the pane keeps for saying what just
	// happened, which can take a sentence and a hint.
	settingsNoticeLines = 2
	// settingsChrome is the lines every settings pane spends besides its rows
	// and its notice: the top edge, the rule above the notice and the bottom edge.
	settingsChrome = 3
)

// settingItem is one toggleable setting in the s pane. enabled reads it, set
// applies it in memory and saves it, and after runs once it has, returning
// anything else the reader should know and any work the change starts.
//
// The behaviour hangs off the item rather than off a kind, so a setting that
// is neither a notification nor a watch option costs an entry in allSettings
// and nothing else.
type settingItem struct {
	section string
	setting string
	detail  string
	enabled func(a *App) bool
	set     func(a *App, on bool) error
	after   func(a *App, on bool) (warning string, cmd tea.Cmd)
}

// allSettings lists every setting prutil can change for itself in the pane. The
// notification rows are built from notifications, which stays the one list of
// what prutil can notify about, so a new notification appears here by itself.
var allSettings = buildSettings()

func buildSettings() []settingItem {
	out := make([]settingItem, 0, len(notifications)+1)
	for _, n := range notifications {
		out = append(out, settingItem{
			section: "DESKTOP NOTIFICATIONS",
			setting: n.setting,
			detail:  n.detail,
			enabled: func(a *App) bool { return a.homeCfg.Notifications.Enabled(n.event) },
			set: func(a *App, on bool) error {
				a.homeCfg.Notifications.Set(n.event, on)
				if a.store == nil {
					return a.storeErr
				}
				return a.store.SetNotification(n.event, on)
			},
			after: func(a *App, on bool) (string, tea.Cmd) {
				warning := ""
				if on {
					warning = a.settings.unavailable
				}
				// Starts the reads when this was the first notification turned
				// on. The last one turned off ends them at the next wake-up,
				// with no request.
				return warning, a.scheduleNotifications()
			},
		})
	}

	return append(out, settingItem{
		section: "WATCHING",
		setting: "Self-review feedback",
		detail: "Treat every unresolved review comment written from your account as actionable feedback for " +
			"coding agents, apart from the replies your agents left behind.",
		enabled: func(a *App) bool { return a.homeCfg.Watch.SelfReview },
		set: func(a *App, on bool) error {
			a.homeCfg.Watch.SelfReview = on
			if a.store == nil {
				return a.storeErr
			}
			return a.store.SetWatchSelfReview(on)
		},
	})
}

// settingsRow is one line inside the window: either a section header or a setting.
type settingsRow struct {
	heading string
	item    int
}

func settingsRows() ([]settingsRow, []int) {
	rows := make([]settingsRow, 0, len(allSettings)+2)
	rowOf := make([]int, len(allSettings))
	lastSection := ""
	for i, s := range allSettings {
		if s.section != lastSection {
			rows = append(rows, settingsRow{heading: s.section, item: -1})
			lastSection = s.section
		}
		rowOf[i] = len(rows)
		rows = append(rows, settingsRow{item: i})
	}
	return rows, rowOf
}

// settingsPane is the s pane: the settings prutil can change for itself, each
// saved the moment it changes, so there is nothing to confirm and nothing to
// lose by closing it.
type settingsPane struct {
	open   bool
	keys   settingsKeyMap
	cursor int
	offset int
	// notice says what the last key did, and noticeErr whether it went wrong.
	// Moving the selection clears it, so it never describes a row the reader
	// has moved away from.
	notice    string
	noticeErr bool
	// unavailable is why a notification would not appear, asked once when the
	// pane opens. Turning one on is still allowed and saved: the reader may be
	// about to install what is missing.
	unavailable string
}

// settingsLayout is where the pane sits and how its height is spent.
type settingsLayout struct {
	x, y          int
	width, height int
	// inner is the width inside the frame and its padding.
	inner int
	// window is how many setting rows are drawn, detail whether the selected
	// one's explanation fits beneath them, and noticeLines how many lines the
	// notice gets.
	window      int
	detail      bool
	noticeLines int
}

// hinter is a notifier that can say where to look when a notification it
// showed did not appear, which on macOS is a setting nobody would guess.
type hinter interface {
	Hint() string
}

// openSettings shows the settings pane with the first setting selected.
func (a *App) openSettings() {
	a.settings = settingsPane{open: true, keys: defaultSettingsKeys()}
	if a.notifier == nil {
		a.settings.unavailable = "prutil cannot show desktop notifications here"
	} else if err := a.notifier.Available(); err != nil {
		a.settings.unavailable = err.Error()
	}
	a.clampSettingsScroll()
}

// closeSettings puts the pane away. Everything it changed is already saved.
func (a *App) closeSettings() {
	a.settings = settingsPane{}
}

// updateSettings handles the messages the pane takes for itself while it is
// open, and reports whether it took this one. Keys and the mouse belong to it;
// replies from GitHub go on to the app as usual.
func (a *App) updateSettings(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a.handleSettingsKey(msg), true
	case tea.PasteMsg:
		return nil, true
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			a.moveSettings(-wheelStep)
		case tea.MouseWheelDown:
			a.moveSettings(wheelStep)
		}
		return nil, true
	case tea.MouseClickMsg:
		return a.clickSettings(msg), true
	}
	return nil, false
}

// handleSettingsKey applies a key press to the open pane.
func (a *App) handleSettingsKey(msg tea.KeyPressMsg) tea.Cmd {
	keys := a.settings.keys
	switch {
	case key.Matches(msg, keys.Quit):
		return tea.Quit
	case key.Matches(msg, keys.Close):
		a.closeSettings()
	case key.Matches(msg, keys.Toggle):
		return a.toggleSetting()
	case key.Matches(msg, keys.Test):
		return a.testNotification()
	case key.Matches(msg, keys.Up):
		a.moveSettings(-1)
	case key.Matches(msg, keys.Down):
		a.moveSettings(1)
	}
	return nil
}

// clickSettings toggles the setting a left click lands on. A click anywhere
// else lands on the pane and does nothing, rather than on the list behind it.
func (a *App) clickSettings(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	l := a.settingsLayout()
	// The rows start beneath the top edge.
	row := msg.Y - l.y - 1
	if msg.X < l.x || msg.X >= l.x+l.width || row < 0 || row >= l.window {
		return nil
	}
	rows, _ := settingsRows()
	index := a.settings.offset + row
	if index >= len(rows) {
		return nil
	}
	item := rows[index].item
	if item < 0 {
		return nil
	}
	a.settings.cursor = item
	return a.toggleSetting()
}

// moveSettings steps the selection by delta, clamped to the list.
func (a *App) moveSettings(delta int) {
	s := &a.settings
	cursor := min(max(s.cursor+delta, 0), len(allSettings)-1)
	if cursor != s.cursor {
		s.notice, s.noticeErr = "", false
	}
	s.cursor = cursor
	a.clampSettingsScroll()
}

// clampSettingsScroll keeps the selected row inside the drawn window.
func (a *App) clampSettingsScroll() {
	s := &a.settings
	if len(allSettings) == 0 {
		s.cursor, s.offset = 0, 0
		return
	}
	rows, rowOf := settingsRows()
	window := a.settingsLayout().window
	row := rowOf[s.cursor]
	s.offset = clampOffset(s.offset, row, window, len(rows))
	// Scrolling up onto the first entry of a section brings its heading with
	// it, so the reader is never shown an entry without knowing where it is.
	if row > 0 && s.offset == row && rows[row-1].item < 0 && window > 1 {
		s.offset = row - 1
	}
}

// toggleSetting turns the selected setting on or off and saves it.
//
// The change applies straight away whether or not it could be saved, and the
// notice says which: a reader who cannot save still wants the setting
// they just asked for, for as long as this prutil runs.
func (a *App) toggleSetting() tea.Cmd {
	item := allSettings[a.settings.cursor]
	on := !item.enabled(a)
	err := item.set(a, on)

	warning, cmd := "", tea.Cmd(nil)
	if item.after != nil {
		warning, cmd = item.after(a, on)
	}

	state := "off"
	if on {
		state = "on"
	}
	switch {
	case err != nil:
		a.settings.setNotice(fmt.Sprintf("%s is %s until prutil quits, but was not saved: %s", item.setting, state, err), true)
	case warning != "":
		a.settings.setNotice(fmt.Sprintf("%s is on and saved, but nothing will appear: %s", item.setting, warning), true)
	default:
		a.settings.setNotice(fmt.Sprintf("%s is %s · saved", item.setting, state), false)
	}
	return cmd
}

// setNotice replaces what the pane says about the last thing it did.
func (s *settingsPane) setNotice(text string, isErr bool) {
	s.notice, s.noticeErr = text, isErr
}

// testNotification shows a sample notification, which is the quickest way to
// find out whether the operating system will show prutil's at all.
func (a *App) testNotification() tea.Cmd {
	if a.notifier == nil {
		a.settings.setNotice("prutil cannot show desktop notifications here", true)
		return nil
	}
	a.settings.setNotice("sending a test notification…", false)
	notifier := a.notifier
	return func() tea.Msg {
		return settingsTestMsg{err: notifier.Notify(desktop.Notification{
			Title: "Test notification",
			Body:  "This is how prutil will tell you that a pull request has changed.",
		})}
	}
}

// applySettingsTest reports how the test notification went, in the pane if it
// is still open and on the status line if not.
func (a *App) applySettingsTest(msg settingsTestMsg) tea.Cmd {
	text, isErr := "sent a test notification", false
	if msg.err != nil {
		text, isErr = "the test notification failed: "+msg.err.Error(), true
	} else if h, ok := a.notifier.(hinter); ok && h.Hint() != "" {
		text += ". Nothing appeared? " + h.Hint()
	}
	if !a.settings.open {
		return status(text)
	}
	a.settings.setNotice(text, isErr)
	return nil
}

// settingsLayout sizes and places the pane for the current terminal, giving
// up the explanation first and then half the notice when height is short.
func (a *App) settingsLayout() settingsLayout {
	l := settingsLayout{width: a.width, detail: true, noticeLines: settingsNoticeLines}
	room := a.height
	if a.floating() {
		l.width = min(a.width-4, settingsMaxWidth)
		room -= 2
	}
	l.inner = max(l.width-4, 1)

	rows, _ := settingsRows()
	totalRows := len(rows)
	fixed := func() int {
		n := settingsChrome + l.noticeLines
		if l.detail {
			n += 1 + settingsDetailLines
		}
		return n
	}
	if fixed()+totalRows > room {
		l.detail = false
	}
	if fixed()+totalRows > room {
		l.noticeLines = 1
	}
	l.window = max(min(totalRows, room-fixed()), 1)
	l.height = fixed() + l.window
	l.x = max((a.width-l.width)/2, 0)
	l.y = max((a.height-l.height)/2, 0)
	return l
}

// renderSettings draws the pane over a finished screen.
func (a *App) renderSettings(base []string) []string {
	l := a.settingsLayout()
	return a.floatOver(base, a.settingsBox(l), l.x, l.y, l.width)
}

// settingsBox draws the pane itself, frame and all, as l.height lines of
// l.width columns.
func (a *App) settingsBox(l settingsLayout) []string {
	s := &a.settings
	rows, _ := settingsRows()
	box := make([]string, 0, l.height)
	box = append(box,
		a.edge(l.width, "╭", "╮", a.styles.OverlayTitle.Render("Settings"), a.styles.Muted.Render(a.configLabel())),
	)
	for i := 0; i < l.window; i++ {
		line, index := "", s.offset+i
		if index < len(rows) {
			r := rows[index]
			if r.item < 0 {
				line = "  " + a.styles.SectionHdr.Render(r.heading)
			} else {
				line = a.settingLine(allSettings[r.item], r.item == s.cursor, l.inner)
			}
		}
		box = append(box, a.frameRow(line, l.inner))
	}

	if l.detail {
		box = append(box, a.frameRule(l.width))
		lines := wrapLines(allSettings[s.cursor].detail, l.inner, settingsDetailLines)
		for i := 0; i < settingsDetailLines; i++ {
			text := ""
			if i < len(lines) {
				text = lines[i]
			}
			box = append(box, a.frameRow(a.styles.Muted.Render(text), l.inner))
		}
	}

	box = append(box, a.frameRule(l.width))
	text, style := a.settingsNotice()
	lines := wrapLines(text, l.inner, l.noticeLines)
	for i := 0; i < l.noticeLines; i++ {
		line := ""
		if i < len(lines) {
			line = style.Render(lines[i])
		}
		box = append(box, a.frameRow(line, l.inner))
	}
	return append(box, a.edge(l.width, "╰", "╯", a.settingsHints(), ""))
}

// settingLine draws one toggle: the selection bar, a box that is ticked when
// the setting is on, its name, and on or off at the far end, so the state
// is written out as well as coloured.
func (a *App) settingLine(item settingItem, selected bool, width int) string {
	prefix, titleStyle := "  ", a.styles.Text
	if selected {
		prefix, titleStyle = a.styles.SelectBar.Render("▌")+" ", a.styles.Title
	}
	box, state, stateStyle := "[ ]", "off", a.styles.Muted
	if item.enabled(a) {
		box, state, stateStyle = "[✓]", "on", a.styles.Success
	}

	room := max(width-2-lenOf(box)-1, 1)
	title := titleStyle.Render(truncatePlain(item.setting, max(room-lenOf(state)-2, 1)))
	return prefix + stateStyle.Render(box) + " " + justify(room, title, stateStyle.Render(state))
}

// settingsNotice is what the notice lines say, in order of what matters: what
// the last key did, then why nothing would appear, then a failed read, and
// otherwise how the pane works.
func (a *App) settingsNotice() (string, lipgloss.Style) {
	s := &a.settings
	switch {
	case s.notice != "" && s.noticeErr:
		return s.notice, a.styles.Error
	case s.notice != "":
		return s.notice, a.styles.Success
	case s.unavailable != "":
		return s.unavailable, a.styles.Error
	case a.notifyErr != nil && a.homeCfg.Notifications.Any():
		return "the last check for changes failed: " + a.notifyErr.Error(), a.styles.Error
	}
	return "Changes are saved as you make them. " +
		"prutil checks your open pull requests every " + humanInterval(a.notifyInterval()) +
		" while a notification is on.", a.styles.Muted
}

// humanInterval writes a poll interval the way a sentence would.
func humanInterval(d time.Duration) string {
	switch {
	case d == time.Minute:
		return "minute"
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d > 0 && d < time.Minute && d%time.Second == 0:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	return model.HumanDuration(d)
}

// configLabel names the file the pane saves to, with the home directory
// written as ~ so that it fits in the frame.
func (a *App) configLabel() string {
	if a.store == nil {
		return "not saved"
	}
	path := a.store.Path(home.ConfigFile)
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

// settingsHints names the pane's own keys, for its bottom edge.
func (a *App) settingsHints() string {
	k := a.settings.keys
	pairs := []key.Help{
		{Key: k.Up.Help().Key + k.Down.Help().Key, Desc: "select"},
		k.Toggle.Help(),
		k.Test.Help(),
		k.Close.Help(),
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, a.styles.OverlayKey.Render(p.Key)+" "+a.styles.Muted.Render(p.Desc))
	}
	return strings.Join(parts, a.styles.Muted.Render(" · "))
}

// settingsTestMsg reports how the test notification went.
type settingsTestMsg struct {
	err error
}
