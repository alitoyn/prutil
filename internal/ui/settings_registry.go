package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/relloyd/prutil/internal/home"
)

// settingKind describes the interactive control type for a setting.
type settingKind int

const (
	settingKindBool settingKind = iota
	settingKindEnum
	settingKindDuration
	settingKindInt
	settingKindString
	settingKindTemplate
	settingKindMap
	settingKindList
)

// settingDescriptor defines a configurable setting in the settings panel.
//
// Adding a new setting to prutil:
//  1. Add the field to the appropriate Config struct in internal/home/config.go.
//  2. Add an entry to allSettings in this file with its section, title, detail, kind,
//     path, accessors, and validators.
//
// The settings pane will automatically display, navigate, edit, and persist it.
type settingDescriptor struct {
	id          string
	section     string
	title       string
	detail      string
	defaultText string
	kind        settingKind
	enumValues  []string
	path        []string

	minDuration  time.Duration
	stepDuration time.Duration
	minInt       int
	stepInt      int

	// getDisplay returns the formatted value string shown on the right side of the row.
	getDisplay func(a *App) string
	// getRaw returns the editable string used to pre-fill inline textinput.
	getRaw func(a *App) string
	// isDefault reports whether the setting currently equals its built-in default.
	isDefault func(a *App) bool
	// isEnabled reports whether the boolean setting is on (for settingKindBool).
	isEnabled func(a *App) bool
	// toggle flips a boolean setting and saves it.
	toggle func(a *App) tea.Cmd
	// cycle advances or retreats an enum setting and saves it.
	cycle func(a *App, delta int) tea.Cmd
	// step increments or decrements a duration or int setting and saves it.
	step func(a *App, delta int) tea.Cmd
	// saveInput validates and saves an inline textinput entry.
	saveInput func(a *App, input string) error
	// reset reverts the setting to its built-in default in config.yaml and memory.
	reset func(a *App) error
}

// allSettings returns the complete registry of settings displayed in the pane,
// grouped by their section headers.
func allSettings() []settingDescriptor {
	defCfg := home.DefaultConfig()

	return []settingDescriptor{
		// ---------------------------------------------------------------------
		// DESKTOP NOTIFICATIONS
		// ---------------------------------------------------------------------
		{
			id:          "notifications.approved",
			section:     "DESKTOP NOTIFICATIONS",
			title:       "Pull request approved",
			detail:      "When one of your open pull requests is approved: its review decision turns to approved or, in a repository without review rules, it gets its first approval.",
			defaultText: "on",
			kind:        settingKindBool,
			path:        []string{"notifications", "events", "approved"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Notifications.Enabled(home.NotifyApproved) {
					return "on"
				}
				return "off"
			},
			getRaw: func(a *App) string {
				return strconv.FormatBool(a.homeCfg.Notifications.Enabled(home.NotifyApproved))
			},
			isEnabled: func(a *App) bool {
				return a.homeCfg.Notifications.Enabled(home.NotifyApproved)
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Notifications.Enabled(home.NotifyApproved) == defCfg.Notifications.Enabled(home.NotifyApproved)
			},
			toggle: func(a *App) tea.Cmd {
				on := !a.homeCfg.Notifications.Enabled(home.NotifyApproved)
				a.homeCfg.Notifications.Set(home.NotifyApproved, on)
				state := "off"
				if on {
					state = "on"
				}
				if a.store != nil {
					if err := a.store.SetNotification(home.NotifyApproved, on); err != nil {
						a.settings.setNotice(fmt.Sprintf("Pull request approved is %s until prutil quits, but was not saved: %s", state, err), true)
					} else if on && a.settings.unavailable != "" {
						a.settings.setNotice(fmt.Sprintf("Pull request approved is on and saved, but nothing will appear: %s", a.settings.unavailable), true)
					} else {
						a.settings.setNotice(fmt.Sprintf("Pull request approved is %s · saved", state), false)
					}
				} else if a.storeErr != nil {
					a.settings.setNotice(fmt.Sprintf("Pull request approved is %s until prutil quits, but was not saved: %s", state, a.storeErr), true)
				} else {
					a.settings.setNotice(fmt.Sprintf("Pull request approved is %s", state), false)
				}
				return a.scheduleNotifications()
			},
			reset: func(a *App) error {
				a.homeCfg.Notifications.Set(home.NotifyApproved, defCfg.Notifications.Enabled(home.NotifyApproved))
				if a.store != nil {
					return a.store.SetNotification(home.NotifyApproved, defCfg.Notifications.Enabled(home.NotifyApproved))
				}
				return nil
			},
		},
		{
			id:           "notifications.interval",
			section:      "DESKTOP NOTIFICATIONS",
			title:        "Check poll interval",
			detail:       "How often prutil checks your open pull requests in the background while any notification is enabled.",
			defaultText:  "2m",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 30 * time.Second,
			path:         []string{"notifications", "interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Notifications.Interval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Notifications.Interval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Notifications.Interval == defCfg.Notifications.Interval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Notifications.Interval)
				next := cur + time.Duration(delta)*30*time.Second
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Notifications.Interval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"notifications", "interval"}, valStr, func(c *home.Config) {
						c.Notifications.Interval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Check poll interval set to %s · saved", valStr), false)
				return a.scheduleNotifications()
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration such as 2m or 45s: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Notifications.Interval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"notifications", "interval"}, valStr, func(c *home.Config) {
						c.Notifications.Interval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Check poll interval set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Notifications.Interval = defCfg.Notifications.Interval
				if a.store != nil {
					return a.store.ResetSetting([]string{"notifications", "interval"}, func(c *home.Config) {
						c.Notifications.Interval = defCfg.Notifications.Interval
					})
				}
				return nil
			},
		},

		// ---------------------------------------------------------------------
		// WATCHING & POLLING
		// ---------------------------------------------------------------------
		{
			id:      "watch.self_review",
			section: "WATCHING & POLLING",
			title:   "Self-review feedback",
			detail: "Treat every unresolved review comment written from your account as actionable feedback for " +
				"coding agents, apart from the replies your agents left behind.",
			kind: settingKindBool,
			isEnabled: func(a *App) bool {
				return a.homeCfg.Watch.SelfReview
			},
			toggle: func(a *App) tea.Cmd {
				on := !a.homeCfg.Watch.SelfReview
				a.homeCfg.Watch.SelfReview = on
				if a.store != nil {
					_ = a.store.SetWatchSelfReview(on)
				}
				state := onOff(on)
				a.settings.setNotice(fmt.Sprintf("Self-review feedback is %s · saved", state), false)
				return a.rereadArmedReviews()
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.SelfReview = false
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "self_review"}, func(c *home.Config) {
						c.Watch.SelfReview = false
					})
				}
				return nil
			},
		},
		{
			id:           "watch.active_interval",
			section:      "WATCHING & POLLING",
			title:        "Active poll interval",
			detail:       "How often an armed pull request is polled while checks or workflows are actively running.",
			defaultText:  "30s",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 15 * time.Second,
			path:         []string{"watch", "active_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.ActiveInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.ActiveInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.ActiveInterval == defCfg.Watch.ActiveInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.ActiveInterval)
				next := cur + time.Duration(delta)*15*time.Second
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Watch.ActiveInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "active_interval"}, valStr, func(c *home.Config) {
						c.Watch.ActiveInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Active poll interval set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration such as 30s or 1m: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Watch.ActiveInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "active_interval"}, valStr, func(c *home.Config) {
						c.Watch.ActiveInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Active poll interval set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.ActiveInterval = defCfg.Watch.ActiveInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "active_interval"}, func(c *home.Config) {
						c.Watch.ActiveInterval = defCfg.Watch.ActiveInterval
					})
				}
				return nil
			},
		},
		{
			id:           "watch.base_interval",
			section:      "WATCHING & POLLING",
			title:        "Base poll interval",
			detail:       "Starting polling interval for an armed pull request once all checks have finished running.",
			defaultText:  "2m",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 30 * time.Second,
			path:         []string{"watch", "base_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.BaseInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.BaseInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.BaseInterval == defCfg.Watch.BaseInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.BaseInterval)
				next := cur + time.Duration(delta)*30*time.Second
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Watch.BaseInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "base_interval"}, valStr, func(c *home.Config) {
						c.Watch.BaseInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Base poll interval set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration such as 2m: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Watch.BaseInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "base_interval"}, valStr, func(c *home.Config) {
						c.Watch.BaseInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Base poll interval set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.BaseInterval = defCfg.Watch.BaseInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "base_interval"}, func(c *home.Config) {
						c.Watch.BaseInterval = defCfg.Watch.BaseInterval
					})
				}
				return nil
			},
		},
		{
			id:           "watch.max_interval",
			section:      "WATCHING & POLLING",
			title:        "Max poll backoff cap",
			detail:       "Maximum polling backoff interval reached when an armed pull request remains unchanged.",
			defaultText:  "30m",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 5 * time.Minute,
			path:         []string{"watch", "max_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.MaxInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.MaxInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.MaxInterval == defCfg.Watch.MaxInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.MaxInterval)
				next := cur + time.Duration(delta)*5*time.Minute
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Watch.MaxInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "max_interval"}, valStr, func(c *home.Config) {
						c.Watch.MaxInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save max interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Max poll backoff set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration such as 30m: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Watch.MaxInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "max_interval"}, valStr, func(c *home.Config) {
						c.Watch.MaxInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Max poll backoff set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.MaxInterval = defCfg.Watch.MaxInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "max_interval"}, func(c *home.Config) {
						c.Watch.MaxInterval = defCfg.Watch.MaxInterval
					})
				}
				return nil
			},
		},
		{
			id:           "watch.notified_interval",
			section:      "WATCHING & POLLING",
			title:        "Post-handoff interval",
			detail:       "Initial polling interval after handing review feedback to a coding agent.",
			defaultText:  "10m",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 1 * time.Minute,
			path:         []string{"watch", "notified_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.NotifiedInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.NotifiedInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.NotifiedInterval == defCfg.Watch.NotifiedInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.NotifiedInterval)
				next := cur + time.Duration(delta)*1*time.Minute
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Watch.NotifiedInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "notified_interval"}, valStr, func(c *home.Config) {
						c.Watch.NotifiedInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Post-handoff interval set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Watch.NotifiedInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "notified_interval"}, valStr, func(c *home.Config) {
						c.Watch.NotifiedInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Post-handoff interval set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.NotifiedInterval = defCfg.Watch.NotifiedInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "notified_interval"}, func(c *home.Config) {
						c.Watch.NotifiedInterval = defCfg.Watch.NotifiedInterval
					})
				}
				return nil
			},
		},
		{
			id:           "watch.max_notified_interval",
			section:      "WATCHING & POLLING",
			title:        "Max post-handoff cap",
			detail:       "Maximum polling backoff cap after handing review feedback to a coding agent.",
			defaultText:  "60m",
			kind:         settingKindDuration,
			minDuration:  15 * time.Second,
			stepDuration: 5 * time.Minute,
			path:         []string{"watch", "max_notified_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.MaxNotifiedInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.MaxNotifiedInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.MaxNotifiedInterval == defCfg.Watch.MaxNotifiedInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.MaxNotifiedInterval)
				next := cur + time.Duration(delta)*5*time.Minute
				if next < 15*time.Second {
					next = 15 * time.Second
				}
				a.homeCfg.Watch.MaxNotifiedInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "max_notified_interval"}, valStr, func(c *home.Config) {
						c.Watch.MaxNotifiedInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Max post-handoff cap set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration: %w", err)
				}
				if d < 15*time.Second {
					return fmt.Errorf("minimum interval is 15s")
				}
				a.homeCfg.Watch.MaxNotifiedInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "max_notified_interval"}, valStr, func(c *home.Config) {
						c.Watch.MaxNotifiedInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Max post-handoff cap set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.MaxNotifiedInterval = defCfg.Watch.MaxNotifiedInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "max_notified_interval"}, func(c *home.Config) {
						c.Watch.MaxNotifiedInterval = defCfg.Watch.MaxNotifiedInterval
					})
				}
				return nil
			},
		},
		{
			id:           "watch.idle_interval",
			section:      "WATCHING & POLLING",
			title:        "Agent idle check interval",
			detail:       "How often a target agent is polled over local socket while waiting for it to become idle.",
			defaultText:  "10s",
			kind:         settingKindDuration,
			minDuration:  1 * time.Second,
			stepDuration: 5 * time.Second,
			path:         []string{"watch", "idle_interval"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Watch.IdleInterval.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.IdleInterval.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.IdleInterval == defCfg.Watch.IdleInterval
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Watch.IdleInterval)
				next := cur + time.Duration(delta)*5*time.Second
				if next < 1*time.Second {
					next = 1 * time.Second
				}
				a.homeCfg.Watch.IdleInterval = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "idle_interval"}, valStr, func(c *home.Config) {
						c.Watch.IdleInterval = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Agent idle interval set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration: %w", err)
				}
				if d < 1*time.Second {
					return fmt.Errorf("minimum interval is 1s")
				}
				a.homeCfg.Watch.IdleInterval = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "idle_interval"}, valStr, func(c *home.Config) {
						c.Watch.IdleInterval = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Agent idle interval set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.IdleInterval = defCfg.Watch.IdleInterval
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "idle_interval"}, func(c *home.Config) {
						c.Watch.IdleInterval = defCfg.Watch.IdleInterval
					})
				}
				return nil
			},
		},
		{
			id:          "watch.dormant_after",
			section:     "WATCHING & POLLING",
			title:       "Dormant poll threshold",
			detail:      "How many consecutive polls at max interval with no changes before polling goes dormant.",
			defaultText: "3 polls",
			kind:        settingKindInt,
			minInt:      1,
			stepInt:     1,
			path:        []string{"watch", "dormant_after"},
			getDisplay: func(a *App) string {
				return fmt.Sprintf("%d polls", a.homeCfg.Watch.DormantAfter)
			},
			getRaw: func(a *App) string {
				return strconv.Itoa(a.homeCfg.Watch.DormantAfter)
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.DormantAfter == defCfg.Watch.DormantAfter
			},
			step: func(a *App, delta int) tea.Cmd {
				next := a.homeCfg.Watch.DormantAfter + delta
				if next < 1 {
					next = 1
				}
				a.homeCfg.Watch.DormantAfter = next
				valStr := strconv.Itoa(next)
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "dormant_after"}, valStr, func(c *home.Config) {
						c.Watch.DormantAfter = next
					}); err != nil {
						a.settings.setNotice("could not save threshold: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Dormant threshold set to %d polls · saved", next), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				n, err := strconv.Atoi(strings.TrimSpace(input))
				if err != nil || n < 1 {
					return fmt.Errorf("must be a positive integer >= 1")
				}
				a.homeCfg.Watch.DormantAfter = n
				valStr := strconv.Itoa(n)
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "dormant_after"}, valStr, func(c *home.Config) {
						c.Watch.DormantAfter = n
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Dormant threshold set to %d polls · saved", n), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.DormantAfter = defCfg.Watch.DormantAfter
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "dormant_after"}, func(c *home.Config) {
						c.Watch.DormantAfter = defCfg.Watch.DormantAfter
					})
				}
				return nil
			},
		},
		{
			id:          "watch.force_precise_every",
			section:     "WATCHING & POLLING",
			title:       "Force precise check every",
			detail:      "Poll count interval to run the precise review-thread query regardless of tripwire counters.",
			defaultText: "5 polls",
			kind:        settingKindInt,
			minInt:      1,
			stepInt:     1,
			path:        []string{"watch", "force_precise_every"},
			getDisplay: func(a *App) string {
				return fmt.Sprintf("every %d polls", a.homeCfg.Watch.ForcePreciseEvery)
			},
			getRaw: func(a *App) string {
				return strconv.Itoa(a.homeCfg.Watch.ForcePreciseEvery)
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.ForcePreciseEvery == defCfg.Watch.ForcePreciseEvery
			},
			step: func(a *App, delta int) tea.Cmd {
				next := a.homeCfg.Watch.ForcePreciseEvery + delta
				if next < 1 {
					next = 1
				}
				a.homeCfg.Watch.ForcePreciseEvery = next
				valStr := strconv.Itoa(next)
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "force_precise_every"}, valStr, func(c *home.Config) {
						c.Watch.ForcePreciseEvery = next
					}); err != nil {
						a.settings.setNotice("could not save interval: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Force precise check set to every %d polls · saved", next), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				n, err := strconv.Atoi(strings.TrimSpace(input))
				if err != nil || n < 1 {
					return fmt.Errorf("must be a positive integer >= 1")
				}
				a.homeCfg.Watch.ForcePreciseEvery = n
				valStr := strconv.Itoa(n)
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "force_precise_every"}, valStr, func(c *home.Config) {
						c.Watch.ForcePreciseEvery = n
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Force precise check set to every %d polls · saved", n), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.ForcePreciseEvery = defCfg.Watch.ForcePreciseEvery
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "force_precise_every"}, func(c *home.Config) {
						c.Watch.ForcePreciseEvery = defCfg.Watch.ForcePreciseEvery
					})
				}
				return nil
			},
		},
		{
			id:          "watch.self_test_marker",
			section:     "WATCHING & POLLING",
			title:       "Self-test marker comment",
			detail:      "Review comment marker string treated as feedback when written by yourself. Empty to disable.",
			defaultText: "[prutil-test]",
			kind:        settingKindString,
			path:        []string{"watch", "self_test_marker"},
			getDisplay: func(a *App) string {
				m := a.homeCfg.Watch.Marker()
				if m == "" {
					return "(disabled)"
				}
				return m
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Watch.Marker()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Watch.Marker() == defCfg.Watch.Marker()
			},
			saveInput: func(a *App, input string) error {
				marker := strings.TrimSpace(input)
				a.homeCfg.Watch.SelfTestMarker = &marker
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"watch", "self_test_marker"}, strconv.Quote(marker), func(c *home.Config) {
						c.Watch.SelfTestMarker = &marker
					}); err != nil {
						return err
					}
				}
				if marker == "" {
					a.settings.setNotice("Self-test marker disabled · saved", false)
				} else {
					a.settings.setNotice(fmt.Sprintf("Self-test marker set to %q · saved", marker), false)
				}
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Watch.SelfTestMarker = defCfg.Watch.SelfTestMarker
				if a.store != nil {
					return a.store.ResetSetting([]string{"watch", "self_test_marker"}, func(c *home.Config) {
						c.Watch.SelfTestMarker = defCfg.Watch.SelfTestMarker
					})
				}
				return nil
			},
		},

		// ---------------------------------------------------------------------
		// AI REVIEW TRIGGER
		// ---------------------------------------------------------------------
		{
			id:          "review.comment",
			section:     "AI REVIEW TRIGGER",
			title:       "Default review comment",
			detail:      "Comment posted to an open pull request when pressing R to trigger an AI review. Empty to disable.",
			defaultText: "/gemini review",
			kind:        settingKindString,
			path:        []string{"review", "comment"},
			getDisplay: func(a *App) string {
				c := a.homeCfg.Review.CommentFor("")
				if c == "" {
					return "(disabled)"
				}
				return c
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Review.CommentFor("")
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Review.CommentFor("") == defCfg.Review.CommentFor("")
			},
			saveInput: func(a *App, input string) error {
				comment := strings.TrimSpace(input)
				a.homeCfg.Review.Comment = &comment
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"review", "comment"}, strconv.Quote(comment), func(c *home.Config) {
						c.Review.Comment = &comment
					}); err != nil {
						return err
					}
				}
				if comment == "" {
					a.settings.setNotice("Review comment trigger disabled · saved", false)
				} else {
					a.settings.setNotice(fmt.Sprintf("Review comment set to %q · saved", comment), false)
				}
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Review.Comment = defCfg.Review.Comment
				if a.store != nil {
					return a.store.ResetSetting([]string{"review", "comment"}, func(c *home.Config) {
						c.Review.Comment = defCfg.Review.Comment
					})
				}
				return nil
			},
		},
		{
			id:          "review.repos",
			section:     "AI REVIEW TRIGGER",
			title:       "Repository comment overrides",
			detail:      "Per-repository review comment overrides (e.g. owner/repo -> @coderabbitai review). Press enter to manage.",
			defaultText: "0 overrides",
			kind:        settingKindMap,
			path:        []string{"review", "repos"},
			getDisplay: func(a *App) string {
				n := len(a.homeCfg.Review.Repos)
				if n == 1 {
					return "1 override"
				}
				return fmt.Sprintf("%d overrides", n)
			},
			getRaw: func(a *App) string {
				return ""
			},
			isDefault: func(a *App) bool {
				return len(a.homeCfg.Review.Repos) == 0
			},
		},

		// ---------------------------------------------------------------------
		// CODING AGENT (HERDR)
		// ---------------------------------------------------------------------
		{
			id:          "herdr.fallback",
			section:     "CODING AGENT (HERDR)",
			title:       "Fallback provisioning strategy",
			detail:      "Fallback strategy when no active agent matches PR branch: 'new' (provision fresh workspace), 'none' (take no action), 'repo' (use any agent in repository).",
			defaultText: "new",
			kind:        settingKindEnum,
			enumValues:  []string{"new", "none", "repo"},
			path:        []string{"herdr", "fallback"},
			getDisplay: func(a *App) string {
				return fmt.Sprintf("« %s »", a.homeCfg.Herdr.Fallback)
			},
			getRaw: func(a *App) string {
				return string(a.homeCfg.Herdr.Fallback)
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.Fallback == defCfg.Herdr.Fallback
			},
			cycle: func(a *App, delta int) tea.Cmd {
				choices := []home.FallbackStrategy{home.FallbackNew, home.FallbackNone, home.FallbackRepo}
				idx := slices.Index(choices, a.homeCfg.Herdr.Fallback)
				if idx < 0 {
					idx = 0
				}
				nextIdx := (idx + delta) % len(choices)
				if nextIdx < 0 {
					nextIdx += len(choices)
				}
				next := choices[nextIdx]
				a.homeCfg.Herdr.Fallback = next
				valStr := string(next)
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "fallback"}, valStr, func(c *home.Config) {
						c.Herdr.Fallback = next
					}); err != nil {
						a.settings.setNotice("could not save fallback: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Fallback strategy set to %q · saved", next), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.Fallback = defCfg.Herdr.Fallback
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "fallback"}, func(c *home.Config) {
						c.Herdr.Fallback = defCfg.Herdr.Fallback
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.agent_kind",
			section:     "CODING AGENT (HERDR)",
			title:       "Agent kind filter",
			detail:      "Restrict handoffs to one kind of agent (such as 'claude' or 'copilot'). Empty allows any agent.",
			defaultText: "(any)",
			kind:        settingKindString,
			path:        []string{"herdr", "agent_kind"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.AgentKind == "" {
					return "(any)"
				}
				return a.homeCfg.Herdr.AgentKind
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Herdr.AgentKind
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.AgentKind == defCfg.Herdr.AgentKind
			},
			saveInput: func(a *App, input string) error {
				kind := strings.TrimSpace(input)
				a.homeCfg.Herdr.AgentKind = kind
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "agent_kind"}, strconv.Quote(kind), func(c *home.Config) {
						c.Herdr.AgentKind = kind
					}); err != nil {
						return err
					}
				}
				if kind == "" {
					a.settings.setNotice("Agent kind filter cleared (any agent allowed) · saved", false)
				} else {
					a.settings.setNotice(fmt.Sprintf("Agent kind filter set to %q · saved", kind), false)
				}
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.AgentKind = defCfg.Herdr.AgentKind
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "agent_kind"}, func(c *home.Config) {
						c.Herdr.AgentKind = defCfg.Herdr.AgentKind
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.skill",
			section:     "CODING AGENT (HERDR)",
			title:       "Herdr skill name",
			detail:      "Skill name invoked in the default prompt when handing off review feedback (e.g. 'triage').",
			defaultText: "(none)",
			kind:        settingKindString,
			path:        []string{"herdr", "skill"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.Skill == "" {
					return "(none)"
				}
				return a.homeCfg.Herdr.Skill
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Herdr.Skill
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.Skill == defCfg.Herdr.Skill
			},
			saveInput: func(a *App, input string) error {
				skill := strings.TrimSpace(input)
				a.homeCfg.Herdr.Skill = skill
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "skill"}, strconv.Quote(skill), func(c *home.Config) {
						c.Herdr.Skill = skill
					}); err != nil {
						return err
					}
				}
				if skill == "" {
					a.settings.setNotice("Herdr skill cleared · saved", false)
				} else {
					a.settings.setNotice(fmt.Sprintf("Herdr skill set to %q · saved", skill), false)
				}
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.Skill = defCfg.Herdr.Skill
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "skill"}, func(c *home.Config) {
						c.Herdr.Skill = defCfg.Herdr.Skill
					})
				}
				return nil
			},
		},
		{
			id:           "herdr.wait_for_idle",
			section:      "CODING AGENT (HERDR)",
			title:        "Wait for agent idle timeout",
			detail:       "How long prutil waits for a working agent to settle before falling back to a notification.",
			defaultText:  "15m",
			kind:         settingKindDuration,
			minDuration:  0,
			stepDuration: 1 * time.Minute,
			path:         []string{"herdr", "wait_for_idle"},
			getDisplay: func(a *App) string {
				return a.homeCfg.Herdr.WaitForIdle.String()
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Herdr.WaitForIdle.String()
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.WaitForIdle == defCfg.Herdr.WaitForIdle
			},
			step: func(a *App, delta int) tea.Cmd {
				cur := time.Duration(a.homeCfg.Herdr.WaitForIdle)
				next := cur + time.Duration(delta)*1*time.Minute
				if next < 0 {
					next = 0
				}
				a.homeCfg.Herdr.WaitForIdle = home.Duration(next)
				valStr := next.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "wait_for_idle"}, valStr, func(c *home.Config) {
						c.Herdr.WaitForIdle = home.Duration(next)
					}); err != nil {
						a.settings.setNotice("could not save timeout: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Wait for idle timeout set to %s · saved", valStr), false)
				return nil
			},
			saveInput: func(a *App, input string) error {
				d, err := time.ParseDuration(strings.TrimSpace(input))
				if err != nil {
					return fmt.Errorf("not a duration: %w", err)
				}
				a.homeCfg.Herdr.WaitForIdle = home.Duration(d)
				valStr := d.String()
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "wait_for_idle"}, valStr, func(c *home.Config) {
						c.Herdr.WaitForIdle = home.Duration(d)
					}); err != nil {
						return err
					}
				}
				a.settings.setNotice(fmt.Sprintf("Wait for idle timeout set to %s · saved", valStr), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.WaitForIdle = defCfg.Herdr.WaitForIdle
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "wait_for_idle"}, func(c *home.Config) {
						c.Herdr.WaitForIdle = defCfg.Herdr.WaitForIdle
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.dry_run",
			section:     "CODING AGENT (HERDR)",
			title:       "Dry run mode",
			detail:      "Record handoffs in logs and notifications without actually sending them to the agent.",
			defaultText: "off",
			kind:        settingKindBool,
			path:        []string{"herdr", "dry_run"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.DryRun {
					return "on"
				}
				return "off"
			},
			getRaw: func(a *App) string {
				return strconv.FormatBool(a.homeCfg.Herdr.DryRun)
			},
			isEnabled: func(a *App) bool {
				return a.homeCfg.Herdr.DryRun
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.DryRun == defCfg.Herdr.DryRun
			},
			toggle: func(a *App) tea.Cmd {
				next := !a.homeCfg.Herdr.DryRun
				a.homeCfg.Herdr.DryRun = next
				state := "off"
				if next {
					state = "on"
				}
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "dry_run"}, strconv.FormatBool(next), func(c *home.Config) {
						c.Herdr.DryRun = next
					}); err != nil {
						a.settings.setNotice("could not save dry run: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Dry run mode is %s · saved", state), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.DryRun = defCfg.Herdr.DryRun
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "dry_run"}, func(c *home.Config) {
						c.Herdr.DryRun = defCfg.Herdr.DryRun
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.toast",
			section:     "CODING AGENT (HERDR)",
			title:       "Herdr toast notifications",
			detail:      "Show herdr's desktop notification alongside each agent handoff.",
			defaultText: "on",
			kind:        settingKindBool,
			path:        []string{"herdr", "toast"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.Toast {
					return "on"
				}
				return "off"
			},
			getRaw: func(a *App) string {
				return strconv.FormatBool(a.homeCfg.Herdr.Toast)
			},
			isEnabled: func(a *App) bool {
				return a.homeCfg.Herdr.Toast
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.Toast == defCfg.Herdr.Toast
			},
			toggle: func(a *App) tea.Cmd {
				next := !a.homeCfg.Herdr.Toast
				a.homeCfg.Herdr.Toast = next
				state := "off"
				if next {
					state = "on"
				}
				if a.store != nil {
					if err := a.store.SaveSetting([]string{"herdr", "toast"}, strconv.FormatBool(next), func(c *home.Config) {
						c.Herdr.Toast = next
					}); err != nil {
						a.settings.setNotice("could not save toast setting: "+err.Error(), true)
						return nil
					}
				}
				a.settings.setNotice(fmt.Sprintf("Herdr toast notifications %s · saved", state), false)
				return nil
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.Toast = defCfg.Herdr.Toast
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "toast"}, func(c *home.Config) {
						c.Herdr.Toast = defCfg.Herdr.Toast
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.prompt",
			section:     "CODING AGENT (HERDR)",
			title:       "Review handoff prompt",
			detail:      "Template rendered with Repo, Number, URL, Title, HeadRef, BaseRef, UnresolvedCount, Note. Press enter to preview, e to edit in $EDITOR.",
			defaultText: "(default template)",
			kind:        settingKindTemplate,
			path:        []string{"herdr", "prompt"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.Prompt == home.DefaultPrompt {
					return "(default)"
				}
				return "(custom template)"
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Herdr.Prompt
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.Prompt == home.DefaultPrompt
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.Prompt = home.DefaultPrompt
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "prompt"}, func(c *home.Config) {
						c.Herdr.Prompt = home.DefaultPrompt
					})
				}
				return nil
			},
		},
		{
			id:          "herdr.check_prompt",
			section:     "CODING AGENT (HERDR)",
			title:       "Failed checks prompt",
			detail:      "Template rendered for failed-check investigations with Repo, Number, URL, Checks, Note. Press enter to preview, e to edit in $EDITOR.",
			defaultText: "(default template)",
			kind:        settingKindTemplate,
			path:        []string{"herdr", "check_prompt"},
			getDisplay: func(a *App) string {
				if a.homeCfg.Herdr.CheckPrompt == home.DefaultCheckPrompt {
					return "(default)"
				}
				return "(custom template)"
			},
			getRaw: func(a *App) string {
				return a.homeCfg.Herdr.CheckPrompt
			},
			isDefault: func(a *App) bool {
				return a.homeCfg.Herdr.CheckPrompt == home.DefaultCheckPrompt
			},
			reset: func(a *App) error {
				a.homeCfg.Herdr.CheckPrompt = home.DefaultCheckPrompt
				if a.store != nil {
					return a.store.ResetSetting([]string{"herdr", "check_prompt"}, func(c *home.Config) {
						c.Herdr.CheckPrompt = home.DefaultCheckPrompt
					})
				}
				return nil
			},
		},

		// ---------------------------------------------------------------------
		// REPOSITORIES & DISCOVERY
		// ---------------------------------------------------------------------
		{
			id:          "repos",
			section:     "REPOSITORIES & DISCOVERY",
			title:       "Explicit repository paths",
			detail:      "Map GitHub repository (owner/name) to explicit local checkout directory path. Press enter to manage.",
			defaultText: "0 paths",
			kind:        settingKindMap,
			path:        []string{"repos"},
			getDisplay: func(a *App) string {
				n := len(a.homeCfg.Repos)
				if n == 1 {
					return "1 path"
				}
				return fmt.Sprintf("%d paths", n)
			},
			getRaw: func(a *App) string {
				return ""
			},
			isDefault: func(a *App) bool {
				return len(a.homeCfg.Repos) == 0
			},
		},
		{
			id:          "discovery.roots",
			section:     "REPOSITORIES & DISCOVERY",
			title:       "Checkout discovery roots",
			detail:      "Root directories scanned to discover git checkouts by matching remote origin URLs. Press enter to manage.",
			defaultText: "0 roots",
			kind:        settingKindList,
			path:        []string{"discovery", "roots"},
			getDisplay: func(a *App) string {
				n := len(a.homeCfg.Discovery.Roots)
				if n == 1 {
					return "1 root"
				}
				return fmt.Sprintf("%d roots", n)
			},
			getRaw: func(a *App) string {
				return ""
			},
			isDefault: func(a *App) bool {
				return len(a.homeCfg.Discovery.Roots) == 0
			},
		},
	}
}

// onOff returns "on" for true and "off" for false.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
