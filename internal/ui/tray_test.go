package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/relloyd/prutil/internal/model"
)

func TestRecordTrayNotification(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	require.Empty(t, app.tray.items)
	assert.Zero(t, app.tray.unreadCount())

	k1 := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k1, "PR title 1", "Passed checks", "All CI checks passed", trayKindChecks)

	require.Len(t, app.tray.items, 1)
	assert.Equal(t, 1, app.tray.unreadCount())
	assert.Equal(t, k1, app.tray.items[0].key)
	assert.Equal(t, "PR title 1", app.tray.items[0].title)
	assert.Equal(t, "Passed checks", app.tray.items[0].headline)
	assert.Equal(t, "All CI checks passed", app.tray.items[0].detail)
	assert.Equal(t, trayKindChecks, app.tray.items[0].kind)
	assert.False(t, app.tray.items[0].read)

	k2 := model.Key{Repo: "relloyd/prutil", Number: 2}
	app.recordTrayNotification(k2, "PR title 2", "Approved", "Changes approved", trayKindApproved)

	require.Len(t, app.tray.items, 2)
	assert.Equal(t, 2, app.tray.unreadCount())
	assert.Equal(t, k2, app.tray.items[0].key, "newest item is prepended")
	assert.Equal(t, k1, app.tray.items[1].key)
}

func TestTrayCapacityCappedAtMaxTrayItems(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	for i := 0; i < maxTrayItems+15; i++ {
		k := model.Key{Repo: "relloyd/prutil", Number: i}
		app.recordTrayNotification(k, fmt.Sprintf("PR %d", i), "Notice", "Detail", trayKindDesktop)
	}

	assert.Len(t, app.tray.items, maxTrayItems, "tray items should be capped at maxTrayItems")
	assert.Equal(t, model.Key{Repo: "relloyd/prutil", Number: maxTrayItems + 14}, app.tray.items[0].key)
}

func TestTrayMarkAllRead(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	k := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k, "Title", "Headline", "Body", trayKindChecks)
	require.Equal(t, 1, app.tray.unreadCount())

	app.tray.markAllRead()
	assert.Zero(t, app.tray.unreadCount())
	assert.True(t, app.tray.items[0].read)
}

func TestToggleNotificationsInPopupMode(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	k := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k, "Title", "Headline", "Body", trayKindChecks)
	assert.False(t, app.tray.popupOpen)
	assert.Equal(t, 1, app.tray.unreadCount())

	send(t, app, press("n"))
	assert.True(t, app.tray.popupOpen, "pressing n opens popup")
	assert.Zero(t, app.tray.unreadCount(), "opening popup marks notifications read")

	send(t, app, press("n"))
	assert.False(t, app.tray.popupOpen, "pressing n again closes popup")
}

func TestTrayPopupNavigationAndKeys(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	for i := 0; i < 5; i++ {
		k := model.Key{Repo: "relloyd/prutil", Number: i}
		app.recordTrayNotification(k, fmt.Sprintf("PR %d", i), "Note", "Body", trayKindDesktop)
	}

	send(t, app, press("n"))
	require.True(t, app.tray.popupOpen)
	assert.Equal(t, 0, app.tray.cursor)

	send(t, app, press("j"))
	assert.Equal(t, 1, app.tray.cursor)

	send(t, app, press("down"))
	assert.Equal(t, 2, app.tray.cursor)

	send(t, app, press("k"))
	assert.Equal(t, 1, app.tray.cursor)

	send(t, app, press("up"))
	assert.Equal(t, 0, app.tray.cursor)

	send(t, app, press("G"))
	assert.Equal(t, 4, app.tray.cursor)

	send(t, app, press("g"))
	assert.Equal(t, 0, app.tray.cursor)

	send(t, app, press("esc"))
	assert.False(t, app.tray.popupOpen, "esc closes popup")

	send(t, app, press("n"))
	require.True(t, app.tray.popupOpen)
	send(t, app, press("q"))
	assert.False(t, app.tray.popupOpen, "q closes popup")
}

func TestTrayPopupPinTogglesSidebar(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	send(t, app, press("n"))
	require.True(t, app.tray.popupOpen)
	assert.False(t, app.homeCfg.Notifications.Sidebar)

	send(t, app, press("p"))
	assert.False(t, app.tray.popupOpen, "pinning closes popup")
	assert.True(t, app.homeCfg.Notifications.Sidebar, "pinning enables sidebar setting")
	assert.True(t, app.sidebarVisible())
	assert.Equal(t, paneNotifications, app.focus, "pinning shifts focus to notification pane")
}

func TestSidebarModeNavigationAndToggle(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	app.homeCfg.Notifications.Sidebar = true
	require.True(t, app.sidebarVisible())

	assert.Equal(t, paneList, app.focus)

	// In sidebar mode, pressing 'n' toggles focus to sidebar
	send(t, app, press("n"))
	assert.Equal(t, paneNotifications, app.focus)
	assert.False(t, app.tray.popupOpen)

	// Pressing 'n' again toggles focus back to list
	send(t, app, press("n"))
	assert.Equal(t, paneList, app.focus)

	// Test directional focus movement: List -> Detail -> Notifications
	send(t, app, press("l"))
	assert.Equal(t, paneDetail, app.focus)
	send(t, app, press("l"))
	assert.Equal(t, paneNotifications, app.focus)

	// Test Back navigation: Notifications -> Detail -> List
	send(t, app, press("h"))
	assert.Equal(t, paneDetail, app.focus)
	send(t, app, press("h"))
	assert.Equal(t, paneList, app.focus)

	// Pressing 'p' while notifications pane is focused unpins sidebar
	send(t, app, press("n"))
	assert.Equal(t, paneNotifications, app.focus)
	send(t, app, press("p"))
	assert.False(t, app.homeCfg.Notifications.Sidebar)
	assert.False(t, app.sidebarVisible())
	assert.Equal(t, paneList, app.focus)
}

func TestOpenNotificationPRJumpsToPR(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	require.Len(t, app.cur().prs, 3)
	// Sample PRs: index 0 is PR 3, index 1 is PR 7, index 2 is PR 42
	targetKey := app.cur().prs[2].Key()

	app.recordTrayNotification(targetKey, "Target PR", "Check pass", "CI passed", trayKindChecks)

	// Open popup
	send(t, app, press("n"))
	require.True(t, app.tray.popupOpen)
	assert.Equal(t, 0, app.tray.cursor)

	// Press enter
	send(t, app, press("enter"))
	assert.False(t, app.tray.popupOpen, "popup should close after jumping")
	assert.Equal(t, paneList, app.focus, "focus should move to list")
	assert.Equal(t, 2, app.cur().cursor, "list cursor should select target PR")
	assert.Equal(t, targetKey, app.selectedKey())
}

func TestOpenNotificationPRSwitchesViewIfPRInDifferentTab(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	closedKey := model.Key{Repo: "relloyd/prutil", Number: 999}
	closedPR := model.PullRequest{
		Repo:   "relloyd/prutil",
		Number: 999,
		Title:  "Closed PR",
		State:  model.PRStateClosed,
	}
	app.views[viewClosed].prs = []model.PullRequest{closedPR}

	app.recordTrayNotification(closedKey, "Closed PR", "Approved", "Approved", trayKindApproved)

	require.Equal(t, viewOpen, app.active)

	send(t, app, press("n"))
	require.True(t, app.tray.popupOpen)

	send(t, app, press("enter"))
	assert.Equal(t, viewClosed, app.active, "should switch to closed view")
	assert.Equal(t, 0, app.cur().cursor)
	assert.Equal(t, closedKey, app.selectedKey())
}

func TestSidebarVisibilityRespectsWindowWidth(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	app.homeCfg.Notifications.Sidebar = true

	assert.True(t, app.sidebarVisible(), "sidebar is visible at width 120")
	lw, dw, tw := app.bodyWidths()
	assert.True(t, tw >= minTrayWidth)
	assert.Equal(t, 36, tw, "sidebar target is 30% width capped at 44")
	assert.Equal(t, 120, lw+dw+tw+4)

	// Resize below 92 columns
	send(t, app, tea.WindowSizeMsg{Width: 91, Height: 40})
	assert.False(t, app.sidebarVisible(), "sidebar should hide below 92 columns")

	lw2, dw2, tw2 := app.bodyWidths()
	assert.Zero(t, tw2)
	assert.Equal(t, 91, lw2+dw2+paneGap)
}

func TestClockMinutesShownWithTray(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	app.views[viewOpen].prs = nil
	assert.False(t, app.minutesShown())

	k := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k, "Title", "Headline", "Body", trayKindChecks)

	// Items exist, but neither popup nor sidebar visible
	assert.False(t, app.minutesShown())

	// Popup opened
	app.tray.popupOpen = true
	assert.True(t, app.minutesShown(), "clock should show minutes when popup is open and has items")

	// Popup closed, sidebar enabled
	app.tray.popupOpen = false
	app.homeCfg.Notifications.Sidebar = true
	assert.True(t, app.minutesShown(), "clock should show minutes when sidebar is visible and has items")
}

func TestTrayHeaderUnreadBadge(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	headerBefore := strings.Join(app.renderHeader(), "\n")
	assert.NotContains(t, headerBefore, "new")

	k := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k, "Title", "Headline", "Body", trayKindChecks)

	headerWithBadge := strings.Join(app.renderHeader(), "\n")
	assert.Contains(t, headerWithBadge, "1 new")

	app.tray.markAllRead()
	headerAfter := strings.Join(app.renderHeader(), "\n")
	assert.NotContains(t, headerAfter, "new")
}

func TestRenderTrayPopupAndSidebar(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	k := model.Key{Repo: "relloyd/prutil", Number: 1}
	app.recordTrayNotification(k, "Sample PR Title", "CI checks passed", "All checks passed", trayKindChecks)

	// Test popup rendering
	popupLines := app.renderTrayPopup([]string{"line1", "line2", "line3", "line4"})
	require.NotEmpty(t, popupLines)

	// Test sidebar rendering
	app.homeCfg.Notifications.Sidebar = true
	sidebarView := app.renderTrayPane(36, 20)
	require.NotEmpty(t, sidebarView)
	joined := strings.Join(sidebarView, "\n")
	assert.Contains(t, joined, "NOTIFICATIONS")
	assert.Contains(t, joined, "CI checks passed")
}

func TestRenderTrayRowLayout(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	k := model.Key{Repo: "relloyd/prutil", Number: 42}
	app.recordTrayNotification(k, "Add notification tray", "handoff sent", "agent started", trayKindHandoff)

	require.Len(t, app.tray.items, 1)
	item := app.tray.items[0]

	// Unselected 3-line row
	lines := app.renderTrayRow(item, 36, false)
	require.Len(t, lines, 3, "notification row should be exactly 3 lines")
	assert.Contains(t, lines[0], "#42")
	assert.Contains(t, lines[0], "relloyd/prutil")
	assert.Contains(t, lines[0], "now")
	assert.Contains(t, lines[1], "»")
	assert.Contains(t, lines[1], "handoff sent")
	assert.Contains(t, lines[2], "Add notification tray")
	assert.NotContains(t, lines[0], "▌")

	// Full detail line fits when given wider column
	wideLines := app.renderTrayRow(item, 60, false)
	assert.Contains(t, wideLines[2], "Add notification tray · agent started")

	// Selected 3-line row has cursor bar on all 3 lines
	selLines := app.renderTrayRow(item, 36, true)
	require.Len(t, selLines, 3)
	assert.Contains(t, selLines[0], "▌")
	assert.Contains(t, selLines[1], "▌")
	assert.Contains(t, selLines[2], "▌")
}
