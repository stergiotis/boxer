package mdeditlink

import (
	"path"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LinkCodec spells mdedit's launch config as the tail of a `boxer:` link
// (ADR-0409 (proposed) §SD3): the tail is this package's grammar after the
// scheme, so `mdedit://<rest>` and `boxer:open/kind/mdeditLaunch/<rest>`
// are one link. Only a config a link can name has a spelling: a snapshot
// document with no handed text, named by its path's last segment.
var LinkCodec = applink.Codec[launchcfg.MdeditLaunch]{
	Format: formatLaunch,
	Parse:  parseLaunch,
	Examples: []launchcfg.MdeditLaunch{
		Launch(Ref{Mount: identifier.TaggedId(0x84000003A3EC7AEA), Snap: time.Unix(0, 1761574725307515194), Path: "notes/plan.md"}),
		Launch(Ref{Mount: identifier.TaggedId(0x84000003A3EC7AEA), Snap: time.Unix(0, 1761574725307515194), Path: "a b/c.md", Mode: ModeRead, Fragment: "^block-1"}),
	},
}

func init() {
	applink.MustRegisterKind(launchcfg.Kind, LinkCodec)
	applink.MustRegisterAlias(Scheme, launchcfg.Kind)
}

// tailPrefix is what a link has before the tail: the alias scheme and the
// first slash of `://`, so the tail starts with the second.
const tailPrefix = Scheme + ":/"

func parseLaunch(tail string) (cfg launchcfg.MdeditLaunch, err error) {
	r, err := Parse(tailPrefix + tail)
	if err != nil {
		return
	}
	cfg = Launch(r)
	return
}

func formatLaunch(cfg launchcfg.MdeditLaunch) (tail string, err error) {
	if cfg.Text != "" || cfg.Path == "" || cfg.Name != path.Base(cfg.Path) {
		err = eb.Build().Errorf("mdeditlink: only a snapshot document, with no handed text, has a link")
		return
	}
	mode, ok := ParseMode(cfg.Mode)
	if !ok {
		err = eb.Build().Str("mode", cfg.Mode).Errorf("mdeditlink: unknown mode")
		return
	}
	link := Format(Ref{Mount: identifier.TaggedId(cfg.Mount), Snap: time.Unix(0, cfg.SnapNanos), Path: cfg.Path, Mode: mode, Fragment: cfg.Location})
	tail = strings.TrimPrefix(link, tailPrefix)
	return
}
