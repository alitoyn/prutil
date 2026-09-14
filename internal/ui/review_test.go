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
)

func TestTriggerAIReviewPostsDefaultComment(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	pr, ok := app.selectedPR()
	require.True(t, ok)

	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.True(t, app.busy(), "a review trigger in flight must keep the spinner active")

	// Settle the initial status message from the batch.
	msgs := drain(cmd)
	for _, msg := range msgs {
		if sm, ok := msg.(statusMsg); ok {
			send(t, app, sm)
		}
	}
	assert.Equal(t, "triggering AI review on relloyd/prutil#42…", app.status)

	require.Len(t, client.comments(), 1)
	assert.Equal(t, pr.Key(), client.comments()[0].key)
	assert.Equal(t, "/gemini review", client.comments()[0].body)

	// Now deliver the asynchronous reply message.
	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			for _, reply := range drain(send(t, app, tr)) {
				send(t, app, reply)
			}
		}
	}

	assert.False(t, app.busy(), "work is done once the reply lands")
	assert.Equal(t, "triggered AI review on relloyd/prutil#42 (/gemini review)", app.status)

	activity := app.runtimeOf(pr.Key()).activity
	require.NotEmpty(t, activity)
	assert.Equal(t, "triggered AI review (/gemini review)", activity[len(activity)-1].text)
}

func TestTriggerAIReviewUsesConfiguredComment(t *testing.T) {
	client := newFakeClient(samplePRs(), sampleChecks())
	opener := &fakeOpener{}
	homeCfg := fastWatch()
	homeCfg.Review = home.ReviewConfig{Comment: "/gemini review --full"}
	app := New(Config{
		Client:    client,
		Opener:    opener,
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
	drain(cmd)

	require.Len(t, client.comments(), 1)
	assert.Equal(t, "/gemini review --full", client.comments()[0].body)
}

func TestTriggerAIReviewPreventsDuplicateRequestsInFlight(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	pr, ok := app.selectedPR()
	require.True(t, ok)

	app.mutate(pr.Key()).triggeringReview = true

	cmd := send(t, app, press("R"))
	require.NotNil(t, cmd)
	assert.Equal(t, statusMsg("already triggering AI review for relloyd/prutil#42"), cmd())
	assert.Empty(t, client.comments(), "no request should be sent when already in flight")
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

func TestTriggerAIReviewHandlesClientError(t *testing.T) {
	app, client, _ := newTestApp(t, 120, 40)
	client.commentErr = errors.New("github 500 error")
	pr, ok := app.selectedPR()
	require.True(t, ok)

	cmd := send(t, app, press("R"))
	msgs := drain(cmd)
	for _, msg := range msgs {
		if tr, ok := msg.(triggerReviewMsg); ok {
			for _, reply := range drain(send(t, app, tr)) {
				send(t, app, reply)
			}
		}
	}

	assert.False(t, app.busy())
	assert.Equal(t, "could not trigger AI review on relloyd/prutil#42: github 500 error", app.status)

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
