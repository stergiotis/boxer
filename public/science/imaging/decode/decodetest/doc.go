// Package decodetest is the conformance kit of the reader seam (ADR-0398
// §SD9): a reader's own tests run [Check] over a few valid files of its
// format, and it checks what ADR-0398 §SD5–§SD8 and the decode package's
// doc ask of every reader — that it describes itself completely, recognises
// its files and no others, lists entries that decode to what they announce,
// refuses over each limit before reading samples, stops when cancelled,
// wraps the seam's refusals, survives every truncation of a valid file, and
// does not call a failing read a malformed file. It cannot check that a
// format was read correctly: that is for the reader's fixtures, against a
// reference reading of the same files.
//
//	func TestConformance(t *testing.T) {
//		decodetest.Check(t, myformat.Reader(),
//			decodetest.File{Name: "small.mf", Data: small},
//			decodetest.File{Name: "pages.mf", Data: pages, Entries: []decodetest.Expect{
//				{Id: "0", Width: 64, Height: 48, Planes: 3, Kind: imaging.SampleKindU16},
//				{Id: "1", Unreadable: true},
//			}})
//	}
//
// Give it files whose samples are larger than their headers by a few
// kilobytes: the check that a limit is refused before the samples are read
// says nothing of a file it could read whole within its bound. Give it, too,
// a file with no readable entry where the format has one — a file of
// tables, an archive of other members: its default is refused as announced,
// and the checks that need a stack say in the log that they were skipped.
//
// The bound on what a refusal over a limit reads is relative to what
// Entries reads. A reader of a compressed container reads more to reach a
// member's header than to list it — see [File.HeaderSlack] — and a reader
// that has to decompress a file whole to list it reads everything in
// Entries already, so the bound proves nothing of it: such a reader owes a
// test of its own that a decode over a limit stops before it holds the
// samples, by what it decompresses rather than by what it reads.
//
// The three [decode.Limits] are the caller's, and the limits check holds a
// reader to them exactly: one below what a stack needs is refused, exactly
// the need decodes. A reader may keep bounds of its own — the size of a
// chunk or a tile it holds at once, the bytes it inflates to list a file —
// and refuses past one with decode.OverLimit, the sentence naming the
// bound as the reader's own rather than as one of the caller's. Such a
// bound must not refuse a file whose stack the caller's limits admit
// merely because one chunk is larger than a plane, as a small image
// written as one tile larger than itself is. So derive it from the
// request rather than fix it: no lower than what the caller's limits
// admit — MaxPixels for a chunk's pixels, MaxBytes for its bytes — and no
// lower than a floor that holds the largest chunk the format's writers put
// around a small image. A chunk past both is then past anything the caller
// asked for, and the exact limits still decode.
//
// The kit also carries a format of its own, written by [Write] and read by
// [Reader], so that this tree's tests can open stacks, several entries,
// placements, invalid values and refusals through a registry without a
// real format. It is not an interchange format and is not meant to be
// registered by a host.
package decodetest
