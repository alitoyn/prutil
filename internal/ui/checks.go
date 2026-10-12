package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/relloyd/prutil/internal/home"
	"github.com/relloyd/prutil/internal/model"
)

// applyEffectiveChecks shares the complete detail verdict with every
// consumer of checks. A raw failing rollup alone does not establish failure.
func (a *App) applyEffectiveChecks(key model.Key, head string, checks []model.Check, at time.Time) tea.Cmd {
	if at.IsZero() {
		at = a.now()
	}
	status := model.CountChecks(checks).Rollup()
	if status == model.StatusUnknown {
		return nil
	}

	var notices tea.Cmd
	if _, ok := a.prByKey(key); ok {
		notices = a.notice([]reading{{
			key: key, at: at,
			facts: prFacts{head: head, passed: status == model.StatusSuccess, checksKnown: true},
		}})
	}
	if !a.armed(key) || head == "" {
		return notices
	}
	entry := a.mutate(key)
	entry.headOID, entry.headAt, entry.rollup = head, at, status
	a.engine.ReconcileChecks(key, head, status, a.now())
	return tea.Batch(notices, a.scheduleWatch(), a.postOnPass(key, head, status))
}

func (a *App) clearStaleCheckRead(key model.Key, head string, at time.Time) {
	state, ok := a.checks[key]
	if ok && (state.headOID == "" || state.headOID == head) &&
		(state.askedAt.IsZero() || !at.Before(state.askedAt)) {
		delete(a.checks, key)
	}
}

// resolveFailureChecks fetches detail only for ambiguous rollups when the
// checks-passed notification is enabled. The cheap list and snapshot reads
// remain unchanged, and a watched PR already receives this read on its poll.
func (a *App) resolveFailureChecks(key model.Key) tea.Cmd {
	if !a.homeCfg.Notifications.Enabled(home.NotifyChecksPassed) || a.armed(key) {
		return nil
	}
	if a.checks[key].loading {
		return nil
	}
	state := a.checks[key]
	state.loading = true
	state.askedAt = a.now()
	a.checks[key] = state
	return a.loadChecks(a.gen, key)
}

func (a *App) needsCheckResolution(key model.Key, head string, raw model.Status) bool {
	if raw == model.StatusFailure {
		return true
	}
	state, ok := a.checks[key]
	if !ok || !state.loaded || raw == model.StatusUnknown ||
		(state.headOID != "" && state.headOID != head) {
		return false
	}
	if state.err != nil {
		return true
	}
	return len(state.checks) > 0 && model.CountChecks(state.checks).Rollup() != raw
}

// provisionalCheckStatus withholds actions when a cheap rollup conflicts with
// detail we have already read, or when the detail is incomplete.
func (a *App) provisionalCheckStatus(key model.Key, head string, raw model.Status) model.Status {
	if raw == model.StatusFailure {
		return model.StatusUnknown
	}
	state, ok := a.checks[key]
	if !ok || !state.loaded || state.headOID != "" && state.headOID != head {
		return raw
	}
	if state.err != nil || len(state.checks) > 0 &&
		model.CountChecks(state.checks).Rollup() != raw {
		return model.StatusUnknown
	}
	return raw
}

func (a *App) resolveFailedPRs(prs []model.PullRequest) tea.Cmd {
	var cmds []tea.Cmd
	for _, pr := range prs {
		if a.needsCheckResolution(pr.Key(), pr.HeadOID, pr.Rollup) {
			cmds = append(cmds, a.resolveFailureChecks(pr.Key()))
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) resolveFailedSnapshots(snaps []model.Snapshot) tea.Cmd {
	var cmds []tea.Cmd
	for _, snap := range snaps {
		if snap.Key.Repo != "" && a.needsCheckResolution(snap.Key, snap.HeadOID, snap.Rollup) {
			cmds = append(cmds, a.resolveFailureChecks(snap.Key))
		}
	}
	return tea.Batch(cmds...)
}
