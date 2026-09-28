package fsbroker_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func newAppDataSetup(t *testing.T) (inst *inprocbus.Inst, svc *fsbroker.Service, root string) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(500 * time.Millisecond)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	root = t.TempDir()
	svc.SetAppDataRoot(root)
	return
}

func appDataClient(inst *inprocbus.Inst, id app.AppIdT) (c *fsbroker.AppDataClient) {
	return fsbroker.NewAppDataClient(inst.NewClient(id, []app.SubjectFilter{
		{Pattern: fsbroker.SubjectAppDataPrefix + ">", Direction: app.CapDirectionPub, Reason: "test app data area"},
	}))
}

func TestAppData_WriteReadAppendStatList(t *testing.T) {
	inst, _, root := newAppDataSetup(t)
	c := appDataClient(inst, "example.test/some/app")

	w, err := c.Write("plan.json", []byte("{}\n"))
	require.NoError(t, err)
	want := filepath.Join(root, "example.test", "some", "app", "plan.json")
	assert.Equal(t, want, w.Location)
	assert.EqualValues(t, 3, w.Entry.Size)

	data, err := c.ReadFile("plan.json")
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(data))

	require.NoError(t, c.WriteFile("plan.json", []byte("{\"v\":2}\n")))
	data, err = c.ReadFile("plan.json")
	require.NoError(t, err)
	assert.Equal(t, "{\"v\":2}\n", string(data), "a write replaces the file")

	require.NoError(t, c.AppendFile("plan.json.journal", []byte("a\n")))
	require.NoError(t, c.AppendFile("plan.json.journal", []byte("b\n")))
	data, err = c.ReadFile("plan.json.journal")
	require.NoError(t, err)
	assert.Equal(t, "a\nb\n", string(data))

	st, err := c.Stat("plan.json")
	require.NoError(t, err)
	assert.False(t, st.NotExist)
	assert.Equal(t, want, st.Location)

	entries, err := c.List()
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	assert.ElementsMatch(t, []string{"plan.json", "plan.json.journal"}, names)
}

func TestAppData_MissingFile(t *testing.T) {
	inst, _, _ := newAppDataSetup(t)
	c := appDataClient(inst, "example.test/app")

	_, err := c.ReadFile("absent.json")
	require.ErrorIs(t, err, fs.ErrNotExist)

	st, err := c.Stat("absent.json")
	require.NoError(t, err, "a stat of a missing file is an answer")
	assert.True(t, st.NotExist)

	entries, err := c.List()
	require.NoError(t, err, "an area never written is empty")
	assert.Empty(t, entries)
}

// A name is one path component, so no request can reach outside the area,
// and nothing starting with a dot, which is where replace's temporary files
// live.
func TestAppData_RefusesNamesThatAreNotOneComponent(t *testing.T) {
	inst, _, root := newAppDataSetup(t)
	c := appDataClient(inst, "example.test/app")
	for _, name := range []string{"", "../escape", "a/b", ".hidden", "..", "a\\b", "sp ace"} {
		err := c.WriteFile(name, []byte("x"))
		assert.Errorf(t, err, "name %q", name)
		assert.NotErrorIs(t, err, fs.ErrNotExist)
	}
	_, err := os.Stat(filepath.Join(root, "escape"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// Each app sees only its own area: the directory is keyed on the bus's
// sender, not on anything in the payload.
func TestAppData_AreasAreSeparateByApp(t *testing.T) {
	inst, _, _ := newAppDataSetup(t)
	a := appDataClient(inst, "example.test/a")
	b := appDataClient(inst, "example.test/b")
	require.NoError(t, a.WriteFile("x.json", []byte("a")))
	_, err := b.ReadFile("x.json")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	entries, err := b.List()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAppData_NeedsTheCap(t *testing.T) {
	inst, _, _ := newAppDataSetup(t)
	c := fsbroker.NewAppDataClient(inst.NewClient("example.test/nocap", nil))
	err := c.WriteFile("x.json", []byte("x"))
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation)
}

func TestAppData_NoRootRefuses(t *testing.T) {
	inst, svc, _ := newAppDataSetup(t)
	svc.SetAppDataRoot("")
	c := appDataClient(inst, "example.test/app")
	err := c.WriteFile("x.json", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no app data directory")
}
