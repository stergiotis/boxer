package pickerbridge

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	boxerenv "github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
)

func newBridge(t *testing.T, cfg Config) (b *Bridge, svc *fsbroker.Service, cleanup func()) {
	t.Helper()
	inst := inprocbus.NewInst(zerolog.Nop())
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	b = NewBridge(svc, zerolog.Nop(), cfg)
	cleanup = func() {
		svc.Close()
	}
	return
}

func TestNewBridge_DefaultsRootedAtSlash(t *testing.T) {
	b, _, cleanup := newBridge(t, Config{})
	defer cleanup()
	require.NotNil(t, b)
	assert.Equal(t, "/", b.cfg.FsRoot)
	assert.Equal(t, "", b.CurrentRequestId())
}

func TestNewBridge_CustomRoot(t *testing.T) {
	b, _, cleanup := newBridge(t, Config{FsRoot: "/home/test-user"})
	defer cleanup()
	assert.Equal(t, "/home/test-user", b.cfg.FsRoot)
}

func TestBridge_Render_IdleWhenNoPending(t *testing.T) {
	b, _, cleanup := newBridge(t, Config{})
	defer cleanup()
	// No pending. Render should early-return before touching ids — passing
	// nil is safe because no picker.Render path is taken.
	b.Render(nil)
	assert.Equal(t, "", b.CurrentRequestId())
	assert.Nil(t, b.picker)
}

func TestBridge_ToAbsolute_RootedAtSlash(t *testing.T) {
	b := &Bridge{cfg: Config{FsRoot: "/"}}
	assert.Equal(t, "/home/test-user/foo.txt", b.toAbsolute("home/test-user/foo.txt"))
	assert.Equal(t, "/etc/passwd", b.toAbsolute("etc/passwd"))
}

func TestBridge_ToAbsolute_RootedElsewhere(t *testing.T) {
	b := &Bridge{cfg: Config{FsRoot: "/home/test-user"}}
	assert.Equal(t, "/home/test-user/foo.txt", b.toAbsolute("foo.txt"))
	assert.Equal(t, "/home/test-user/a/b/c", b.toAbsolute("a/b/c"))
}

func TestBridge_ToAbsolute_AlreadyAbsolute(t *testing.T) {
	b := &Bridge{cfg: Config{FsRoot: "/"}}
	assert.Equal(t, "/etc/passwd", b.toAbsolute("/etc/passwd"))
}

func TestDefaultStartDir_NoHome(t *testing.T) {
	boxerenv.Home.SetForTest(t, "")
	assert.Equal(t, ".", defaultStartDir("/"))
}

func TestDefaultStartDir_HomeUnderRoot(t *testing.T) {
	boxerenv.Home.SetForTest(t, "/home/test-user")
	assert.Equal(t, "home/test-user", defaultStartDir("/"))
}

func TestDefaultStartDir_HomeOutsideRoot(t *testing.T) {
	boxerenv.Home.SetForTest(t, "/somewhere/else")
	// FsRoot "/srv" is unrelated to HOME — relative would start with ".." → fall back to "."
	assert.Equal(t, ".", defaultStartDir("/srv"))
}

func TestPickerOptionsFor_OpRouting(t *testing.T) {
	cases := []struct {
		op        string
		wantMode  filepicker.ModeE
		wantTitle string
	}{
		// Default (unknown op) → ModeOpen + "Open file". Read explicitly
		// shares the default; declaring it makes the read row visible in
		// the test table.
		{op: "", wantMode: filepicker.ModeOpen, wantTitle: "Open file"},
		{op: "read", wantMode: filepicker.ModeOpen, wantTitle: "Open file"},
		{op: "write", wantMode: filepicker.ModeSave, wantTitle: "Save as"},
		{op: "bundle", wantMode: filepicker.ModePickFolder, wantTitle: "Pick folder"},
		{op: "watch", wantMode: filepicker.ModePickFolder, wantTitle: "Pick folder to watch"},
		// Unknown op should fall through to the default rather than
		// erroring or returning an empty title.
		{op: "future-op-tbd", wantMode: filepicker.ModeOpen, wantTitle: "Open file"},
	}
	for _, tc := range cases {
		t.Run("op="+tc.op, func(t *testing.T) {
			gotMode, gotTitle := pickerOptionsFor(tc.op, "")
			assert.Equal(t, tc.wantMode, gotMode)
			assert.Equal(t, tc.wantTitle, gotTitle)
		})
	}
}

func TestPickerOptionsFor_TitleOverrideWins(t *testing.T) {
	// Override replaces the op-derived title regardless of which op was
	// requested. Mode is never overridden — write keeps ModeSave even
	// when the title is custom.
	_, title := pickerOptionsFor("read", "Custom prompt")
	assert.Equal(t, "Custom prompt", title)

	mode, title := pickerOptionsFor("write", "Pick a destination")
	assert.Equal(t, filepicker.ModeSave, mode, "override must not change mode")
	assert.Equal(t, "Pick a destination", title)

	_, title = pickerOptionsFor("watch", "Watch which folder?")
	assert.Equal(t, "Watch which folder?", title)
}

func TestPickerOptionsFor_EmptyOverrideKeepsDefault(t *testing.T) {
	// Empty override means "use the op-derived default" — guards
	// against a regression where startPicker accidentally consumes the
	// empty string as a title.
	_, title := pickerOptionsFor("bundle", "")
	assert.Equal(t, "Pick folder", title)
}
