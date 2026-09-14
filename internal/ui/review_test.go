package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/relloyd/prutil/internal/home"
	"github.com/relloyd/prutil/internal/model"
	"github.com/relloyd/prutil/internal/watch"
)

func TestTriggerAIReviewRequiresConfirmationAndPostsComment(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	pr, ok := app.selectedPR()
	require.True(t, ok)

	// First press asks for confirmation and does not send a request.
	cmd1 := send(t, app, press("R"))
	require.NotNil(t, cmd1)
	assert.Equal(t, statusMsg("press R again to post /gemini review on relloyd/prutil#42"), cmd1())
	assert.Empty(t, client.comments(), "first press must not post a comment")
	assert.False(t, app.busy())

	// Second press within the confirmation window confirms and dispatches the comment.
	cmd2 := send(t, app, press("R"))
	require.NotNil(t, cmd2)
	assert.True(t, app.busy(), "a review trigger in flight must keep the spinner active")

	// Delivering the in-flight triggerReviewMsg completes the request.
	msgs := drain(cmd2)
	require.Len(t, client.comments(), 1)
	assert.Equal(t, pr.NodeID, client.comments()[0].subjectID)
	assert.Equal(t, "/gemini review", client.comments()[0].body)

	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			send(t, app, tr)
		}
	}

	assert.False(t, app.busy(), "work is done once the reply lands")

	activity := app.runtimeOf(pr.Key()).activity
	require.NotEmpty(t, activity)
	assert.Equal(t, "triggered AI review (/gemini review)", activity[len(activity)-1].text)
}

func TestTriggerAIReviewConfirmationExpiresAfterStatusLifetime(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)

	// First press asks for confirmation.
	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("press R again to post /gemini review on relloyd/prutil#42"), cmd())

	// Time passes beyond the confirmation lifetime.
	advance(app, 5*time.Second)

	// Subsequent press asks for confirmation again instead of triggering.
	cmd2 := send(t, app, press("R"))
	require.NotNil(t, cmd2)
	assert.Equal(t, statusMsg("press R again to post /gemini review on relloyd/prutil#42"), cmd2())
}

func TestTriggerAIReviewUsesPerRepoOverride(t *testing.T) {
	client := newFakeClient(samplePRs(), sampleChecks())
	homeCfg := fastWatch()
	homeCfg.Review = home.ReviewConfig{
		Comment: defaultString("/gemini review"),
		Repos: map[string]string{
			"relloyd/prutil": "@coderabbitai review",
		},
	}
	app := New(Config{
		Client:    client,
		Opener:    &fakeOpener{},
		Clipboard: &fakeClipboard{},
		Now:       func() time.Time { return testNow },
		Store:     home.OpenIn(t.TempDir()),
		State:     home.NewState(),
		Home:      homeCfg,
	})
	send(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	send(t, app, prsMsg{gen: app.gen, prs: samplePRs()})

	// First press confirms with overridden comment.
	cmd1 := send(t, app, press("R"))
	require.NotNil(t, cmd1)
	assert.Equal(t, statusMsg("press R again to post @coderabbitai review on relloyd/prutil#42"), cmd1())

	// Second press executes.
	cmd2 := send(t, app, press("R"))
	require.NotNil(t, cmd2)
	drain(cmd2)

	require.Len(t, client.comments(), 1)
	assert.Equal(t, "@coderabbitai review", client.comments()[0].body)
}

func TestTriggerAIReviewRefusedWhenUnconfiguredOrDisabled(t *testing.T) {
	client := newFakeClient(samplePRs(), sampleChecks())
	homeCfg := fastWatch()
	homeCfg.Review = home.ReviewConfig{
		Comment: defaultString(""),
	}
	app := New(Config{
		Client:    client,
		Opener:    &fakeOpener{},
		Clipboard: &fakeClipboard{},
		Now:       func() time.Time { return testNow },
		Store:     home.OpenIn(t.TempDir()),
		State:     home.NewState(),
		Home:      homeCfg,
	})
	send(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	send(t, app, prsMsg{gen: app.gen, prs: samplePRs()})

	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("AI review is not configured (set review.comment in config)"), cmd())
	assert.Empty(t, client.comments())
}

func TestTriggerAIReviewRefusesDuringHandoffOrReview(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	pr, ok := app.selectedPR()
	require.True(t, ok)

	// Refuse while a handoff is running.
	app.mutate(pr.Key()).handing = true
	app.setWatchOperation(pr.Key(), "handing over")

	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("already handing relloyd/prutil#42 over"), cmd())
	assert.Equal(t, "handing over", app.runtimeOf(pr.Key()).operation, "must not wipe operation label")

	// Refuse while reading review threads.
	app.mutate(pr.Key()).handing = false
	app.mutate(pr.Key()).reviewing = true
	app.setWatchOperation(pr.Key(), "reading review threads")

	cmd = send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("already reading review feedback on relloyd/prutil#42"), cmd())
	assert.Equal(t, "reading review threads", app.runtimeOf(pr.Key()).operation)

	assert.Empty(t, client.comments())
}

func TestTriggerAIReviewPreventsDuplicateRequestsInFlight(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)

	// Confirm to start the first request.
	send(t, app, press("R"))
	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)

	// Pressing R again while still in flight is rejected.
	cmd2 := send(t, app, press("R"))
	require.NotNil(t, cmd2)
	assert.Equal(t, statusMsg("already triggering AI review for relloyd/prutil#42"), cmd2())

	// Settle the in-flight command.
	msgs := drain(cmd)
	require.Len(t, client.comments(), 1, "only the initial request sent")

	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			send(t, app, tr)
		}
	}
}

func TestTriggerAIReviewRefusedOnClosedPR(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	send(t, app, press("tab"))
	require.Equal(t, viewClosed, app.active)

	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("AI review is available only for open pull requests"), cmd())
	assert.Empty(t, client.comments(), "no comment posted for closed pull requests")
}

func TestTriggerAIReviewWakesWatchScheduleOnSuccess(t *testing.T) {
	app, _, _ := newTestApp(t, 120, 40)
	pr, ok := app.selectedPR()
	require.True(t, ok)

	// Arm the pull request and simulate it going dormant in the engine.
	send(t, app, press("w"))
	reading := model.Snapshot{Key: pr.Key(), NodeID: pr.NodeID, HeadOID: "123", Rollup: model.StatusSuccess}
	now := app.now()
	for range 20 {
		app.engine.Observe([]model.Snapshot{reading}, now)
		next, ok := app.engine.NextDue()
		if !ok {
			break
		}
		now = next
	}
	tier, _ := app.engine.Tier(pr.Key())
	require.Equal(t, watch.TierDormant, tier)

	// Trigger AI review.
	send(t, app, press("R"))
	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)

	msgs := drain(cmd)
	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			send(t, app, tr)
		}
	}

	tier, _ = app.engine.Tier(pr.Key())
	assert.Equal(t, watch.TierSettled, tier, "triggering review must wake dormant watcher")
	assert.Equal(t, []model.Key{pr.Key()}, app.engine.Due(app.now()))
}

func TestTriggerAIReviewHandlesClientError(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	client.commentErr = errors.New("github 500 error")
	pr, ok := app.selectedPR()
	require.True(t, ok)

	send(t, app, press("R"))
	cmd := send(t, app, press("R"))
	msgs := drain(cmd)
	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			send(t, app, tr)
		}
	}

	assert.False(t, app.busy())

	activity := app.runtimeOf(pr.Key()).activity
	require.NotEmpty(t, activity)
	assert.Equal(t, "could not trigger AI review: github 500 error", activity[len(activity)-1].text)
}

func TestTriggerAIReviewWhenNoPRSelected(t *testing.T) {
	client := newFakeClient([]model.PullRequest{}, map[model.Key][]model.Check{})
	app := New(Config{
		Client:    client,
		Opener:    &fakeOpener{},
		Clipboard: &fakeClipboard{},
		Now:       func() time.Time { return testNow },
		Store:     home.OpenIn(t.TempDir()),
		State:     home.NewState(),
		Home:      fastWatch(),
	})
	cmd := send(t, app, press("R"))
	assert.Nil(t, cmd)
	assert.Empty(t, client.comments())
}

func defaultString(s string) *string {
	return &s
}
