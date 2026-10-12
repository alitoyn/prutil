package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/relloyd/prutil/internal/home"
	"github.com/relloyd/prutil/internal/model"
)

func TestOldFailuresOnTheCurrentHeadDoNotAppearOrReachAnAgent(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	app.views[viewOpen].prs[0].Rollup = model.StatusFailure
	delete(app.checks, key)

	assert.Equal(t, model.StatusUnknown, app.rollupFor(app.views[viewOpen].prs[0]),
		"the raw historical failure is provisional until detail is complete")

	current := []model.Check{{Name: "Validate PR title", Status: model.StatusSuccess}}
	send(t, app, press("w"))
	// The initial check command was not run; let the explicit poll own the read.
	app.mutate(key).handing = false
	setRollup(client, "abc", model.StatusFailure)
	client.checks[key] = current
	poll(t, app)

	assert.Equal(t, model.CheckCounts{Success: 1, Total: 1}, model.CountChecks(app.checks[key].checks))
	assert.Equal(t, model.StatusSuccess, app.rollupFor(app.views[viewOpen].prs[0]))
	assert.NotContains(t, plain(app.render()), "✗2")
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)),
		"a raw FAILURE containing only superseded failures must not take an agent off feedback")
	assert.NotEmpty(t, dispatcherOf(t, app).requests(), "review feedback still reaches the agent")
}

func TestPartialChecksStayVisibleButCannotTriggerAutomaticActions(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	app.views[viewOpen].prs[0].Rollup = model.StatusFailure
	app.homeCfg.ChecksPassed.Comment = "/deploy staging"
	send(t, app, press("w"))
	poll(t, app)
	app.state.TogglePostOnPass(key.String())

	send(t, app, checksErrMsg{
		gen: app.gen, key: key, headOID: "abc", err: errors.New("page two unavailable"),
		checkHandoff: true, checks: []model.Check{{Name: "Validate PR title", Status: model.StatusFailure}},
	})

	assert.Contains(t, plain(app.render()), "partial checks")
	assert.Contains(t, plain(app.render()), "Validate PR title")
	assert.Equal(t, model.StatusUnknown, app.rollupFor(app.views[viewOpen].prs[0]))
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))

	send(t, app, checksErrMsg{
		gen: app.gen, key: key, headOID: "abc", err: errors.New("page two unavailable"),
		checks: []model.Check{{Name: "Validate PR title", Status: model.StatusSuccess}},
	})
	assert.Empty(t, passComments(app.client.(*fakeClient)), "a partial success does not authorize a deployment comment")
}

func TestManuallySendingPartialChecksNeedsFreshConfirmation(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	app.checks[key] = checkState{
		headOID: "abc", loaded: true, err: errors.New("page two unavailable"),
		checks: []model.Check{{Name: "build", Status: model.StatusFailure}},
	}

	ask := send(t, app, press("F"))
	require.NotNil(t, ask)
	assert.Contains(t, string(ask().(statusMsg)), "press F again to send only the visible failures")
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))

	app.checks[key] = checkState{
		headOID: "abc", loaded: true, err: errors.New("page two unavailable"),
		checks: []model.Check{{Name: "lint", Status: model.StatusFailure}},
	}
	changed := send(t, app, press("F"))
	require.NotNil(t, changed)
	assert.Contains(t, string(changed().(statusMsg)), "checks changed")
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))

	ask = send(t, app, press("F"))
	require.NotNil(t, ask)
	assert.Contains(t, string(ask().(statusMsg)), "press F again")
	investigate(t, app, "F")
	require.Equal(t, 1, checkHandoffs(dispatcherOf(t, app)))
	assert.Equal(t, "lint", lastRequest(t, dispatcherOf(t, app), true).Checks[0].Name)
}

func TestIncompleteChecksWithoutVisibleFailuresCannotBeSentManually(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	app.checks[key] = checkState{
		headOID: "abc", loaded: true, err: errors.New("page two unavailable"),
		checks: []model.Check{{Name: "build", Status: model.StatusSuccess}},
	}

	reply := send(t, app, press("F"))

	require.NotNil(t, reply)
	assert.Contains(t, string(reply().(statusMsg)), "no visible failed checks")
	assert.Zero(t, client.callsFor(key), "an incomplete result must not become an unconfirmed new handoff")
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))
}

func TestPartialChecksAndAnUntrustedReviewNeedSeparateConfirmations(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	client.review = hostileReview()
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	send(t, app, press("w"))
	poll(t, app)
	require.True(t, app.runtimeOf(key).hold.Held())
	app.checks[key] = checkState{
		headOID: "abc", loaded: true, err: errors.New("page two unavailable"),
		checks: []model.Check{{Name: "build", Status: model.StatusFailure}},
	}

	first := send(t, app, press("F"))
	assert.Contains(t, string(first().(statusMsg)), "is held")
	second := send(t, app, press("F"))
	assert.Contains(t, string(second().(statusMsg)), "incomplete checks")
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))
	investigate(t, app, "F")
	assert.Equal(t, 1, checkHandoffs(dispatcherOf(t, app)))
}

func TestStaleHeadDetailCannotOverwriteCurrentChecks(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "new-head"
	app.checks[key] = checkState{
		headOID: "new-head", loaded: true,
		checks: []model.Check{{Name: "build", Status: model.StatusSuccess}},
	}

	send(t, app, checksMsg{
		gen: app.gen, key: key, headOID: "old-head",
		checks: []model.Check{{Name: "build", Status: model.StatusFailure}},
	})

	assert.Equal(t, model.StatusSuccess, app.rollupFor(app.views[viewOpen].prs[0]))
	require.Len(t, app.checks[key].checks, 1, "a stale reply must not discard a newer complete cache entry")
	assert.Equal(t, "new-head", app.checks[key].headOID)
	assert.Zero(t, checkHandoffs(dispatcherOf(t, app)))
}

func TestAResolvedHistoricalFailureCanRaiseAChecksPassedNotification(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	key := samplePRs()[0].Key()
	app.views[viewOpen].prs[0].HeadOID = "abc"
	app.homeCfg.Notifications.Set(home.NotifyChecksPassed, true)
	send(t, app, checksMsg{
		gen: app.gen, key: key, headOID: "abc",
		checks: []model.Check{{Name: "Validate PR title", Status: model.StatusFailure}},
	})
	setRollup(client, "abc", model.StatusFailure)
	snap := client.snapshots["PR_42"]
	snap.Key = key
	send(t, app, notifyPollMsg{at: app.now(), snaps: []model.Snapshot{snap}})

	reply := send(t, app, checksMsg{
		gen: app.gen, key: key, headOID: "abc",
		checks: []model.Check{{Name: "Validate PR title", Status: model.StatusSuccess}},
	})
	require.NotNil(t, reply)
	assert.Contains(t, statuses(drain(reply)), statusMsg("relloyd/prutil#42 checks passed"))
	assert.Len(t, notifierOf(t, app).notifications(), 1)

	send(t, app, notifyPollMsg{at: app.now(), snaps: []model.Snapshot{snap}})
	assert.Len(t, notifierOf(t, app).notifications(), 1, "the unchanged raw failure must not announce a second pass")
}
