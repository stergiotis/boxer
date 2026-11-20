package fsbroker

import (
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ServedRootEnv is the directory the file picker is rooted at on a host
// served over the headless carrier. There the person choosing a file is a
// remote viewer, who has no business browsing the serving machine's whole
// filesystem; the desktop host keeps the picker rooted at "/".
var ServedRootEnv = env.NewPath(env.Spec{
	Name:        "BOXER_FS_PICKER_SERVED_ROOT",
	Description: "fs broker: directory the file picker is rooted at on a host served over the headless carrier (IMZERO2_HEADLESS_LISTEN set); empty uses <user config dir>/boxer/picker, created 0700. The desktop host roots the picker at /",
	Category:    env.CategorySystem,
})

// ServedRoot returns the picker root for a served host — ServedRootEnv,
// else <user config dir>/boxer/picker — creating the directory when it is
// missing. It never returns "/": a served host that cannot make its root has
// no picker root to fall back to and gets the error.
func ServedRoot() (root string, err error) {
	root = ServedRootEnv.Get()
	if root == "" {
		cfg, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			err = eh.Errorf("fsbroker: no user config dir for the served picker root: %w", cfgErr)
			return
		}
		root = filepath.Join(cfg, "boxer", "picker")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		err = eh.Errorf("fsbroker: served picker root: %w", err)
		return
	}
	if root == string(filepath.Separator) {
		err = eh.Errorf("fsbroker: the served picker root must not be the filesystem root")
		return
	}
	err = os.MkdirAll(root, 0o700)
	if err != nil {
		err = eh.Errorf("fsbroker: create the served picker root: %w", err)
	}
	return
}
