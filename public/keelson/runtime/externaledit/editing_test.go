package externaledit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// editingWith is an Editing that has handed base over and receives what
// the test sends.
func editingWith(base string) (*Editing, chan Revision) {
	revs := make(chan Revision, 4)
	return &Editing{id: "e1", base: base, revs: revs, stop: make(chan struct{})}, revs
}

func TestASaveReplacesTheTextWhileItIsUnchangedHere(t *testing.T) {
	ed, revs := editingWith("a")
	revs <- Revision{Seq: 1, Text: []byte("b"), Session: "s1"}
	revs <- Revision{Seq: 2, Text: []byte("c"), Session: "s1"}
	text, replace := ed.Poll("a")
	require.True(t, replace)
	require.Equal(t, "c", text, "the latest of the saves that arrived together is taken")
	require.False(t, ed.Conflict())
	require.Equal(t, uint64(2), ed.Last().Seq)

	revs <- Revision{Seq: 3, Text: []byte("d")}
	text, replace = ed.Poll("c")
	require.True(t, replace)
	require.Equal(t, "d", text)
}

func TestASaveWaitsWhenThePersonEditedHere(t *testing.T) {
	ed, revs := editingWith("a")
	revs <- Revision{Seq: 1, Text: []byte("theirs")}
	_, replace := ed.Poll("mine")
	require.False(t, replace, "the person's edit is never overwritten")
	require.True(t, ed.Conflict())

	text, ok := ed.Take()
	require.True(t, ok)
	require.Equal(t, "theirs", text)
	require.False(t, ed.Conflict())

	revs <- Revision{Seq: 2, Text: []byte("again")}
	_, replace = ed.Poll("mine2")
	require.False(t, replace)
	ed.Keep("mine2")
	require.False(t, ed.Conflict())
	revs <- Revision{Seq: 3, Text: []byte("later")}
	text, replace = ed.Poll("mine2")
	require.True(t, replace, "after Keep, the next save is taken over the kept text")
	require.Equal(t, "later", text)
}

func TestTheEndOfAnEditIsReported(t *testing.T) {
	ed, revs := editingWith("a")
	revs <- Revision{Closed: true, Reason: "the window closed"}
	_, replace := ed.Poll("a")
	require.False(t, replace)
	require.False(t, ed.Active())
	require.Contains(t, ed.Status(), "the window closed")
}
