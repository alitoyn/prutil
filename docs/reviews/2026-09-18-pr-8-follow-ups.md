# PR #8 follow-ups: self-review feedback and the agent marker

What the review of [#8](https://github.com/relloyd/prutil/pull/8) turned up that
is not fixed on this branch. Two of the findings are done and are not listed
here: every rendered prompt now asks for the agent marker, and toggling
self-review reads the watched pull requests again.

## Parked for a decision

### 1. Self-review treats your replies to other reviewers as agent work

`ReviewThread.NeedsAttention` returns true for any unresolved thread whose
newest comment is yours, wherever that thread came from. A reviewer asks "why
not X?", you answer "because Y" and leave it open waiting on them: with
`watch.self_review` on, your answer goes to a coding agent as work.

Requiring `strings.EqualFold(t.Opener, viewer)` would confine the mode to
threads you opened, which is what "self-review" reads as. The counter-argument
is that a note to yourself on somebody else's thread is a normal way to record
work, and that rule would drop it.

**Waiting on:** a conversation with Ali about which of the two the setting is
meant to be. Nothing should change until then.

## To do

### 2. Threads an agent fixes without replying never close

In self-review mode the agent's instruction is to reply only on the threads it
leaves alone, and only the author can resolve their own review threads. So a
thread the agent fixed silently stays unresolved with your comment newest,
which is still feedback under the self-review rule. `Engine.Precise` never sees
`open == 0`, so:

- the pull request stays on the notified backoff (10-60 minutes) rather than
  settling, which delays a real reviewer's next comment;
- the open-threads badge and the `UnresolvedCount` in later prompts stay
  inflated.

`NotifiedThreads` keeps it from being handed over again, so this is a
liveness and honesty problem rather than a loop.

Worth considering: ask the agent to reply on every thread it acts on, not only
the ones it declines, so its own marked reply closes the thread out. That costs
a comment per fix and makes the marker load-bearing for all of them.

### 3. The marker is matched anywhere in a comment body

`model.IsAgentComment` is `strings.Contains(body, AgentCommentMarker)`, while
the prompt asks for the marker on its own line at the end. Writing the marker
inside a code span, which is likely in this repository in particular, takes
that thread off the feedback list silently. Pasting an agent's markdown into a
comment of your own does the same.

Matching the last non-empty line against the marker would keep the prompt's
promise and leave prose about the marker alone.

## Minor, with a proposed fix

### 4. The settings pane shows a notification error under every row

`settingsNotice` falls through to `s.unavailable` ("prutil cannot show desktop
notifications here") and then to `a.notifyErr` whatever row is selected, so both
appear in red beneath `Self-review feedback`, which has nothing to do with
notifications and works fine without a notifier.

**Proposed fix:** give `settingItem` a `warn func(a *App) string`, the standing
caveat for that setting, and have `settingsNotice` consult
`allSettings[s.cursor].warn` rather than reaching for the notification state
itself. The notification rows return `unavailable`, or the failed-poll message;
the self-review row returns nothing. `toggleSetting`'s "on and saved, but
nothing will appear" warning can come from the same place, which drops the
`after` hook's second return value. This keeps the rule in AGENTS.md: the
behaviour hangs off the item, and no field names a kind.

### 5. Two copies of "the newest comment, or the opener if its body is empty"

`agentAnswered` and `selfTestComment` each carry the rule, and they disagree:
`agentAnswered` switches the author to `Opener` along with the body, so a
reviewer's empty-bodied reply on a thread your agent opened reads as answered,
while `selfTestComment` checks `LatestBy` first and never switches.

The fallback looks like it exists for hand-built test values rather than for
anything GitHub returns: `reviewThreadQuery` selects `body` on the `latest`
alias, and a thread holding one comment has the opener as its newest comment
anyway, so `LatestBody` is only empty when the comment itself is.

**Proposed fix:** drop the fallback, give both a single helper returning the
newest comment's author and body, and update the fixtures that only set `Body`
to set the latest fields as GitHub would. If the fallback is worth keeping,
guard it with `t.Comments <= 1` so it can only ever apply to a thread whose
newest comment *is* the opener.

### 6. `WatchConfig.ReviewFilter()` returns a filter that rules nothing out

The returned `model.ReviewFilter` has an empty `Viewer`, which `NeedsAttention`
reads as "every thread is feedback" and which also turns off `agentAnswered`.
It is safe only because `gh.Review.Feedback` fills the viewer in before use, and
the tests already build filters by hand, so the next caller that reaches for
`model.Feedback` directly gets the handoff loop back.

**Proposed fix:** take `Viewer` out of `ReviewFilter`, leaving it the
configuration it is named for (`Marker`, `SelfReview`), and pass the viewer as
its own argument: `model.Feedback(threads, viewer, rules)` and
`thread.NeedsAttention(viewer, rules)`. A filter that cannot hold a viewer
cannot be missing one.
