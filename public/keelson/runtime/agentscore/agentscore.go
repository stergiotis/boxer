// Package agentscore holds the scorecard of one chat conversation: a single
// ClickHouse query over the trail views (trailviews) and the run journal's
// views (journalviews) that answers one row of what the conversation cost,
// how its tool calls and operations ended, what the person decided, how far
// its labels rose and whether its artefact was exported, endorsed or
// refused.
//
// The query reads the views' minted physical columns by name; the package's
// test holds every name it reads to the columns the two packages compose, so
// a renamed or dropped column fails here rather than in a scoring run.
// Scoring a task's content (an answer key, planted defects) is the task
// suite's, beside this row, not part of it.
package agentscore

import (
	_ "embed"
)

//go:embed scorecard.sql
var scorecard string

// ParamConversation is the query parameter naming the conversation, as the
// chat stamps it on an artefact's `chat_conversation`; over HTTP it is sent
// as `param_conv`.
const ParamConversation = "conv"

// ParamPrompts is the query parameter naming how many of the person's
// prompts to score; what the conversation did from the next prompt on is
// left out, so takes that went on past a task's last prompt compare with
// takes that stopped there. Zero scores the whole conversation. Over HTTP it
// is sent as `param_prompts`.
const ParamPrompts = "prompts"

// Scorecard returns the scorecard query. It reads the views in the `boxer`
// database, takes the conversation as {conv:String} and the prompts to score
// as {prompts:UInt32}, and has no FORMAT clause, so the caller chooses one.
func Scorecard() (sql string) {
	return scorecard
}
