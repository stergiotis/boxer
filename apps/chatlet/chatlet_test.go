package chatlet

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/chat"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
)

const validDoc = `---
title: Example
summary: "Do the example thing"
apps: true
rounds: 12
allow: view
changes: ask
reach: host
---

You do the example thing.

` + "```sql\nSELECT 1\n```\n"

var testTopics = []app.TopicT{app.TopicObservability}

// The corpus gate: every committed document parses, validates and makes a
// window. Nothing else stands between a document and a launcher entry.
func TestCorpus_everyDocumentMakesAWindow(t *testing.T) {
	seen := map[string]bool{}
	var n int
	for _, b := range snapshotBooks() {
		presets, errs := parseBook(b)
		require.Empty(t, errs, "book %s", b.id)
		for _, p := range presets {
			n++
			require.False(t, seen[string(p.Manifest.Id)], "duplicate id %s", p.Manifest.Id)
			seen[string(p.Manifest.Id)] = true
			assert.Equal(t, app.KindChatlet, p.Manifest.Kind)
			a, err := chat.NewPreset(p)
			require.NoError(t, err, "document %s", p.Manifest.Id)
			assert.Equal(t, p.Manifest.Id, a.Manifest().Id)
		}
	}
	assert.Positive(t, n)
}

func TestAuditTrail_canRunSqlAndNoFurther(t *testing.T) {
	var found bool
	for _, b := range snapshotBooks() {
		presets, _ := parseBook(b)
		for _, p := range presets {
			if strings.HasSuffix(string(p.Manifest.Id), "/audit-trail") {
				found = true
				assert.Equal(t, "run", p.Allow, "set_sql and run need it")
				assert.Equal(t, "data", p.Reach, "clickhouse:<host> needs it")
				assert.True(t, p.Apps && p.Launch && p.Artefact && p.Questions)
				assert.False(t, p.Desktop)
				assert.Equal(t, 48, p.Rounds)
			}
		}
	}
	assert.True(t, found)
}

// A prompt names views by hand, and nothing else would notice a rename.
func TestAuditTrail_namesOnlyViewsThatExist(t *testing.T) {
	known := trailviews.AllViewNames()
	src, err := bookFS.ReadFile("book/audit-trail.md")
	require.NoError(t, err)
	named := regexp.MustCompile(`\b(?:dm|agg)_trail_[a-z_]+`).FindAllString(string(src), -1)
	require.NotEmpty(t, named)
	for _, v := range named {
		assert.Contains(t, known, v)
	}
}

func TestParse_keepsTheWholeBodyAsThePrompt(t *testing.T) {
	p, err := ParseDocSource("b", "example.md", []byte(validDoc), testTopics)
	require.NoError(t, err)
	assert.Contains(t, p.System, "```sql\nSELECT 1\n```", "a code block stays inside the prompt")
	assert.NotContains(t, p.System, "allow:")
	assert.Equal(t, "b", p.Book)
	assert.Equal(t, app.AppIdT(idPrefix+"example"), p.Manifest.Id)
	assert.Equal(t, "Example", p.Manifest.Display)
	assert.Equal(t, testTopics, p.Manifest.Topics, "the book's default")
	assert.NotEmpty(t, p.Manifest.Caps, "the chat's capabilities")
	assert.Equal(t, 12, p.Rounds)
	assert.True(t, p.Apps)
	assert.False(t, p.Launch, "absent is off")
}

func TestParse_refusals(t *testing.T) {
	for name, c := range map[string]struct{ from, to, want string }{
		"unknown key":  {"rounds: 12", "rounds: 12\nallowed: run", "unknown frontmatter key"},
		"no allow":     {"allow: view\n", "", "is required"},
		"no reach":     {"reach: host\n", "", "is required"},
		"bad allow":    {"allow: view", "allow: everything", "no such allow level"},
		"bad flag":     {"apps: true", "apps: yes please", "must be true or false"},
		"bad rounds":   {"rounds: 12", "rounds: many", "must be a whole number"},
		"bad topic":    {"rounds: 12", "rounds: 12\ntopics: [nonsense]", "unregistered topic"},
		"no title":     {"title: Example\n", "", "title and summary"},
		"negative":     {"rounds: 12", "rounds: -2", "negative"},
		"empty prompt": {"\nYou do the example thing.\n\n```sql\nSELECT 1\n```\n", "\n", "empty prompt"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, validDoc, c.from)
			_, err := ParseDocSource("b", "example.md", []byte(strings.Replace(validDoc, c.from, c.to, 1)), testTopics)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestParse_greetingAndStarters(t *testing.T) {
	doc := strings.Replace(validDoc, "rounds: 12", `rounds: 12
greeting: "Hello"
starters:
  - {label: "First", text: "Do the first thing"}
  - label: "Second"
    text: "Do the second thing"`, 1)
	p, err := ParseDocSource("b", "example.md", []byte(doc), testTopics)
	require.NoError(t, err)
	assert.Equal(t, "Hello", p.Greeting)
	assert.Equal(t, []chat.Starter{{Label: "First", Text: "Do the first thing"}, {Label: "Second", Text: "Do the second thing"}}, p.Starters)
	assert.Equal(t, doc, p.Source)
	assert.NotContains(t, p.System, "Hello")
}

func TestParse_starterRefusals(t *testing.T) {
	for name, c := range map[string]struct{ value, want string }{
		"not a list":    {"starters: nope", "must be a list"},
		"not a mapping": {"starters: [one]", "must have a label and a text"},
		"extra field":   {"starters: [{label: a, text: b, send: true}]", "fields must be strings"},
		"unknown field": {"starters: [{label: a, text: b, send: yes please}]", "only label and text"},
		"no text":       {"starters: [{label: a}]", "needs a label and a text"},
		"greeting":      {"greeting: [a]", "must be a string"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := strings.Replace(validDoc, "rounds: 12", "rounds: 12\n"+c.value, 1)
			_, err := ParseDocSource("b", "example.md", []byte(doc), testTopics)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestMint_oneAppPerDocumentAndABadOneStaysOut(t *testing.T) {
	reg := app.NewRegistry()
	fsys := fstest.MapFS{
		"good.md":  {Data: []byte(validDoc)},
		"bad.md":   {Data: []byte(strings.Replace(validDoc, "allow: view", "allow: all", 1))},
		"note.txt": {Data: []byte("not a document")},
	}
	n, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "b", fsys: fsys, topics: testTopics}})
	assert.Equal(t, 1, n)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "no such allow level")
	ms := reg.Manifests()
	require.Len(t, ms, 1)
	assert.Equal(t, app.AppIdT(idPrefix+"good"), ms[0].Id)
}

func TestRegisterBook_refuses(t *testing.T) {
	require.Error(t, RegisterBook("", fstest.MapFS{}, testTopics))
	require.Error(t, RegisterBook("x", fstest.MapFS{}, nil))
	require.Error(t, RegisterBook("chatlet", fstest.MapFS{}, testTopics), "the package's own book holds the id")
}
