package bindings

import "testing"

// The host derives a dock tab's close id on its side (dock_tab_close_id in
// interpreter/dock_layout.rs) and reports a close under it; CloseRequested
// reads it back under the id derived here. The Rust test asserts the same
// vector, so the two derivations cannot drift apart unnoticed.
func TestDockTabCloseId_matchesTheHost(t *testing.T) {
	const want uint64 = 0x9c92ca95cc497b1c
	if got := dockTabCloseId(0x123456789abcdef0, 3); got != want {
		t.Fatalf("dockTabCloseId: got %#x, want %#x", got, want)
	}
	if dockTabCloseId(1, 3) == dockTabCloseId(3, 1) {
		t.Fatal("a dock id and a tab id that trade places must not share a close id")
	}
}
