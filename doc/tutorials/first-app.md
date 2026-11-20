---
type: tutorial
audience: newcomer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Tutorial: your first app

By the end of this tutorial you will have a module of your own holding one
app — a window with a name field, a **Greet** button and a count of the
greetings that outlives the window — and a host binary that opens it on
your desktop. No knowledge of boxer is assumed beyond the prerequisites.

The app is small on purpose. The same code, under boxer's own module, is
[apps/hello](../../apps/hello/), which compiles and is tested with the rest
of the tree; when this page and that package disagree, the package is
right.

## Prerequisites

- A Linux desktop session (Wayland or X11).
- Go. Boxer's `go.mod` names the version it needs; with the default
  `GOTOOLCHAIN=auto` the `go` command downloads that version on first use.
- git.
- rustup. The window is drawn by a Rust client that you build from a boxer
  checkout; the Rust version is pinned in that checkout and rustup installs
  it on the first build.
- A C compiler, `pkg-config`, and the Wayland and X11 development headers:
  the client's windowing libraries probe for them while building.
- At run time: a Vulkan loader (`libvulkan.so.1`) with a driver for your
  GPU, and fontconfig.
- Optional: the `clickhouse` binary on `PATH`, which `imzero2 demo --list`
  uses to print its table, and a ClickHouse server, which keeps the count
  across processes (without one it lasts as long as the process).

## What we're building

```text
hello/
├── go.mod
├── apps/greet/greet.go          the app: manifest, registration, one window
├── apps/greet/help/overview.md  the page F1 shows
└── cmd/hellohost/main.go        the host binary that opens it
```

## Step 1 — Create the module and pin boxer

Boxer's `main` branch moves quickly and its tags are infrequent, so pin a
commit rather than a tag. Pick a commit of boxer's `main` and use its hash
in both places below; the Go code and the Rust client must come from the
same commit.

```sh
mkdir hello && cd hello
go mod init example.com/hello
go get github.com/stergiotis/boxer@<commit>
```

## Step 2 — Write the app

An app is a Go type implementing `app.AppI` — `Manifest`, `Mount`, `Frame`
and `Unmount` — plus a manifest that describes it, registered with the
default registry when its package is initialised.

In your module, create **apps/greet/greet.go**:

```go
// Package greet is the first app: one window, one value kept across runs,
// and a help page.
package greet

import (
	"context"
	"embed"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

//go:embed help/*.md
var helpFS embed.FS

// AppId is the app's manifest id; its last segment is the launch alias.
const AppId app.AppIdT = "example.com/hello/apps/greet"

// GreetingsKey is the persist key the greeting count is kept under.
const GreetingsKey = "greetings"

var manifest = app.Manifest{
	Id:            AppId,
	Version:       "0.1.0",
	Display:       "Greet",
	Summary:       "Greet someone and count the greetings across runs",
	Topics:        []app.TopicT{app.TopicRuntime},
	Kind:          app.KindDemo,
	Surface:       app.SurfaceWindowed,
	PersistedKeys: []string{GreetingsKey},
	Help:          help.MustSub(helpFS, "help"),
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = &App{name: "world"}
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("greet: failed to register factory")
	}
}

// App is one window.
type App struct {
	ids       *c.WidgetIdStack
	log       zerolog.Logger
	storage   app.StorageI
	name      string
	greetings atomic.Uint64
	scope     *lifetime.Scope
	ownScope  bool
	storeMu   sync.Mutex
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.log = ctx.Log()
	inst.storage = ctx.Storage()
	if raw, found, gErr := inst.storage.Get(GreetingsKey); gErr == nil && found {
		if n, pErr := strconv.ParseUint(string(raw), 10, 64); pErr == nil {
			inst.greetings.Store(n)
		}
	}
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.ownScope {
		inst.scope.Retire()
		<-inst.scope.Done()
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	c.Label("Your name:").Send()
	c.TextEdit(inst.ids.PrepareStr("name"), inst.name, false).
		AccessibleName("Name").
		SendRespVal(&inst.name)
	if c.Button(inst.ids.PrepareStr("greet"), c.Atoms().Text("Greet").Keep()).SendResp().HasPrimaryClicked() {
		inst.greet()
	}
	c.Label("Hello, " + inst.name + "!").Send()
	c.Label("greeted " + strconv.FormatUint(inst.greetings.Load(), 10) + "×").Send()
	return
}

func (inst *App) greet() {
	inst.greetings.Add(1)
	inst.scope.Go(func(context.Context) {
		inst.storeMu.Lock()
		defer inst.storeMu.Unlock()
		n := inst.greetings.Load()
		if sErr := inst.storage.Set(GreetingsKey, []byte(strconv.FormatUint(n, 10))); sErr != nil {
			inst.log.Debug().Err(sErr).Msg("greet: count not stored")
		}
	})
}
```

What each part does:

- **The manifest** names the app (`AppId`; its last segment, `greet`, is the
  alias you launch it by), says it has a window (`SurfaceWindowed`), declares
  the one storage key it uses (`PersistedKeys`), and points at its help
  book. The host grants the app access to exactly the keys it declares.
- **`init`** registers a factory: the host calls it once per window.
- **`Mount`** runs when a window opens. It may block, so it is where the
  stored count is read. Storage is a request to a service, not a local
  variable.
- **`Frame`** runs every frame and must not block. It draws the widgets and
  reacts to the click. The name is bound to the text field through
  `SendRespVal(&inst.name)`; widget ids come from the window's id stack, so
  two open windows do not collide.
- **`greet`** counts the click and writes the count off the frame, on the
  window's lifetime scope, which the host drains when the window closes.

## Step 3 — Write its help page

Create **apps/greet/help/overview.md**. The host shows it when the window has
focus and you press F1.

```markdown
---
type: explanation
audience: end-user
status: draft
title: Greet
---

# Greet

A text field for a name, a **Greet** button, the greeting, and how often
the button was pressed. The count is kept under the key `greetings`.
```

## Step 4 — Write the host binary

The host binary is boxer's `hostmain` with your apps linked in. A blank
import is what registers an app.

Create **cmd/hellohost/main.go**:

```go
package main

import (
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostmain"

	_ "example.com/hello/apps/greet"
)

var host = hostmain.New(hostmain.Options{},
	&cli.Command{Name: "hellohost", Version: vcs.BuildVersionInfo(), Before: logging.Apply})

func main() { host.Main() }
```

Then fetch what the new imports need, and build. The host needs the
`binary_log` build tag, which boxer's own host build adds as well:

```sh
go mod tidy
go build -tags binary_log -o hellohost ./cmd/hellohost
./hellohost imzero2 demo --list
```

The list shows every app linked into the binary — boxer's own and yours —
with `greet` among the aliases. An app missing from it was not imported.

## Step 5 — Build the client

Clone boxer at the commit you pinned and build the desktop client:

```sh
git clone https://github.com/stergiotis/boxer.git ../boxer
git -C ../boxer checkout <commit>
(cd ../boxer/rust/imzero2 && ./build_rust_dist.sh desktop)
```

The binary lands at `../boxer/rust/imzero2/target/dist/imzero2`. The first
build downloads the crates and compiles for several minutes.

## Step 6 — Open the window

```sh
./hellohost imzero2 demo \
    --clientBinary ../boxer/rust/imzero2/target/dist/imzero2 \
    --phosphorFontTTF ../boxer/rust/imzero2/assets/fonts/phosphor/Phosphor.ttf \
    --launch greet
```

A window titled **Greet** opens with "Hello, world!" and `greeted 0×`. Type
a name, press **Greet**, and the count moves. Press F1 for the help page.
The icon font is passed explicitly because the client finds text fonts
through fontconfig but carries its own icon font.

## What you've learned

- An app is a manifest plus an `app.AppI`, registered at package
  initialisation; a host binary links apps by importing them.
- `Mount` may block and `Frame` must not; work that waits goes on the
  window's lifetime scope.
- State that outlives a window is declared in the manifest and read and
  written through the mount context's storage.

## Where to go next

- **Designing a real app** — data as Arrow, an explicit state machine,
  tasks, operations an agent can call:
  [the keelson-app skill](../skills/keelson-app/SKILL.md).
- **Testing it headless** — a scene document opens the app with no display,
  clicks and reads widgets by name, and captures images:
  [the imzero2-drive skill](../skills/imzero2-drive/SKILL.md), and
  [apps/hello/scenes](../../apps/hello/scenes/) for this app's scene.
- **The lint gate and the rest of boxer's standards in your repository**:
  [adopting boxer downstream](../howto/adopting-boxer-downstream.md).
- **Drawing widgets**: [the imzero2 skill](../skills/imzero2/SKILL.md).
