package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The corpus gate: every document of the book parses, and every section
// the chat names is in it.
func TestTheChatPromptBookParses(t *testing.T) {
	defs, errs := promptbook.ParseBook(promptBookId, help.MustSub(promptsFS, "prompts"))
	require.Empty(t, errs)
	slugs := map[string]bool{}
	for _, d := range defs {
		slugs[d.Slug] = true
		assert.NotEmpty(t, d.System, d.Slug)
	}
	for _, sec := range promptSections {
		assert.True(t, slugs[sec.slug], sec.slug)
	}
	for _, slug := range promptNotes {
		assert.True(t, slugs[slug], slug)
	}
	assert.Len(t, defs, len(promptSections)+len(promptNotes), "every document is a section the chat names")
}

// The system prompt is the book's sections in declared order, joined by a
// blank line, each only for the conversations that carry it.
func TestTheSystemPromptIsTheBooksSections(t *testing.T) {
	assert.Equal(t, coordinatorPrompt, systemPromptOf(true, false, false))
	assert.Equal(t, promptText("questions"), systemPrompt(false, true))
	all := systemPromptOf(true, true, true)
	assert.Equal(t, coordinatorPrompt+"\n\n"+promptText("questions")+"\n\n"+promptText("artefact"), all)
	assert.True(t, strings.HasPrefix(lastRoundNote, "You have used every round"))
	assert.True(t, strings.HasPrefix(unattendedNote, "The host runs unattended"))
}

// The fingerprint is stable across renders and moves with any section or
// tool.
func TestThePromptFingerprintMovesWithTheBook(t *testing.T) {
	first, second := promptFingerprint(), promptFingerprint()
	require.Equal(t, first, second)
	require.Len(t, first, 64)

	sections := []string{"a", "b"}
	text := map[string]string{"a": "one", "b": "two"}
	tools := []openaichat.Tool{{Name: "t", Description: "d", Parameters: schema(`{"type":"object"}`)}}
	base := fingerprintOf(sections, func(s string) string { return text[s] }, tools)
	changed := map[string]string{"a": "one", "b": "two."}
	assert.NotEqual(t, base, fingerprintOf(sections, func(s string) string { return changed[s] }, tools))
	assert.NotEqual(t, base, fingerprintOf([]string{"b", "a"}, func(s string) string { return text[s] }, tools), "order is content")
	moved := []openaichat.Tool{{Name: "t", Description: "d", Parameters: schema(`{"type":"object","properties":{}}`)}}
	assert.NotEqual(t, base, fingerprintOf(sections, func(s string) string { return text[s] }, moved))
}

// The transformation book's gate: every document parses with a title, a
// summary and a request, and the slugs mdedit's picker shipped stay.
func TestTheTransformBookParses(t *testing.T) {
	defs, errs := promptbook.ParseBook(transformBookId, help.MustSub(transformsFS, "transforms"))
	require.Empty(t, errs)
	seen := map[string]promptbook.ScopeE{}
	for _, d := range defs {
		assert.NotEmpty(t, d.Title, d.Slug)
		assert.NotEmpty(t, d.Summary, d.Slug)
		assert.NotEmpty(t, d.System, d.Slug)
		seen[d.Slug] = d.Scope
	}
	// Each pinned to the scope its wording assumes.
	assert.Equal(t, map[string]promptbook.ScopeE{
		"improve-style":        promptbook.ScopeSelection,
		"summarize":            promptbook.ScopeDocument,
		"translate-to-english": promptbook.ScopeSelection,
	}, seen)
}

// A transformation's message is its request, then what it works on and the
// operations that read and propose.
func TestTransformRequest(t *testing.T) {
	sel := transformRequest(promptbook.PromptDef{System: "Tighten it.\n", Scope: promptbook.ScopeSelection})
	assert.True(t, strings.HasPrefix(sel, "Tighten it.\n\n"), sel)
	assert.Contains(t, sel, "selection")
	assert.Contains(t, sel, "get_document")
	assert.Contains(t, sel, "propose_edit")
	doc := transformRequest(promptbook.PromptDef{System: "Summarize it.", Scope: promptbook.ScopeDocument})
	assert.Contains(t, doc, "whole document")
	assert.NotContains(t, doc, "selection")
}
