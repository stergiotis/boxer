package agent

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// A coordinator's model works under two bounds (ADR-0280). The grant is what
// the person shared for one task, decided in the host's dialog. The ceiling
// is what the person's settings let the model ask for at all, sent by the
// coordinator and enforced here: the dispatcher refuses a request, a call, a
// launch or a destination above it, whatever the grant says.
//
// Both are scored on one ladder, so a coordinator can draw "how far it could
// go" and "how far it is now" on one scale. The most powerful thing allowed
// picks the level; what else is allowed moves the position within it.

// ReachE is how far agent-caused work may reach beyond the apps' windows.
type ReachE uint8

const (
	// ReachHost: what this host holds — its tables, keelson:<table>, and
	// its local git repositories, git:<path>.
	ReachHost ReachE = 0
	// ReachData: the data endpoints the apps query, clickhouse:<host>.
	ReachData ReachE = 1
	// ReachNetwork: egress destinations and the model service, http:<name>
	// and llm.
	ReachNetwork ReachE = 2
)

// AllReaches lists the reaches, least first.
var AllReaches = []ReachE{ReachHost, ReachData, ReachNetwork}

func (inst ReachE) String() (s string) {
	switch inst {
	case ReachData:
		return "data"
	case ReachNetwork:
		return "network"
	}
	return "host"
}

// ReachOf is the reach a grant destination needs. A destination of a class
// this does not know is taken as the network: the widest, so an unknown
// name is never admitted under a narrower ceiling.
func ReachOf(destination string) (r ReachE) {
	switch {
	case strings.HasPrefix(destination, "keelson:"), strings.HasPrefix(destination, "keelson-bundle:"),
		strings.HasPrefix(destination, "git:"):
		// An ad-hoc bundle (ADR-0288 (proposed) §SD4) is datasets in this
		// process, as a keelson table is.
		return ReachHost
	case strings.HasPrefix(destination, "clickhouse:"):
		return ReachData
	}
	return ReachNetwork
}

// Ceiling is the most a model may do, as one value: what a coordinator's
// settings allow, and — computed by the dispatcher — what a task's grant
// allows right now. The zero value allows nothing in any window: talk only.
type Ceiling struct {
	// Mode is the highest mode a window may be shared in; ModeUnspecified
	// shares none.
	Mode ModeE
	// Effect is the highest effect an operation may have (ADR-0269 §SD5).
	Effect app.OperationEffectE
	// Launch lets the model open windows; Desktop lets it arrange every
	// window (ADR-0276 §SD4).
	Launch  bool
	Desktop bool
	// Reach is how far agent-caused work may reach.
	Reach ReachE
	// Unpaced lets the model's changes land as fast as it makes them. Without
	// it the dispatcher spaces what the person can see — a change to a
	// window, a window opened, an arrangement — so that each can be followed
	// and stopped (§SD6). It is orthogonal to the ladder: speed is not a kind
	// of action.
	Unpaced bool
}

// Unlimited is the ceiling that forbids nothing a grant can hold.
func Unlimited() (c Ceiling) {
	return Ceiling{Mode: ModeAct, Effect: app.OperationEffectConsequential, Launch: true, Desktop: true, Reach: ReachNetwork, Unpaced: true}
}

// normal is c with what cannot apply taken out: without a window nothing
// else is allowed, and observing allows no effect.
func (inst Ceiling) normal() (c Ceiling) {
	c = inst
	switch {
	case c.Mode == ModeUnspecified:
		return Ceiling{}
	case c.Mode == ModeObserve || c.Effect < app.OperationEffectNone:
		c.Effect = app.OperationEffectNone
	}
	return
}

// The refusals. Each is "" when the ceiling allows the thing; a nil ceiling
// allows everything, which is a coordinator that set none.

func (inst *Ceiling) refuseMode(m ModeE) (why string) {
	if inst == nil || m <= inst.Mode {
		return ""
	}
	if inst.Mode == ModeUnspecified {
		return "the chat's settings do not let the model work in windows"
	}
	return "the chat's settings allow at most " + inst.Mode.String() + " mode, not " + m.String()
}

func (inst *Ceiling) refuseEffect(e app.OperationEffectE) (why string) {
	if inst == nil {
		return ""
	}
	c := inst.normal()
	if e <= c.Effect {
		return ""
	}
	return "the chat's settings do not let the model " + effectPhrase(e) + "; they allow at most: " + c.Level().String()
}

func (inst *Ceiling) refuseLaunch() (why string) {
	if inst == nil || inst.normal().Launch {
		return ""
	}
	return "the chat's settings do not let the model open windows"
}

func (inst *Ceiling) refuseDesktop() (why string) {
	if inst == nil || inst.normal().Desktop {
		return ""
	}
	return "the chat's settings do not let the model arrange the desktop"
}

func (inst *Ceiling) refuseDestination(destination string) (why string) {
	if inst == nil {
		return ""
	}
	c := inst.normal()
	if c.Mode != ModeUnspecified && ReachOf(destination) <= c.Reach {
		return ""
	}
	return "the chat's settings do not let the model's work reach " + destination
}

// effectPhrase is what an operation of effect e does, for a refusal.
func effectPhrase(e app.OperationEffectE) (s string) {
	switch e {
	case app.OperationEffectView:
		return "change what a window shows"
	case app.OperationEffectDocument:
		return "edit what a window holds"
	case app.OperationEffectRun:
		return "run against a data source"
	case app.OperationEffectConsequential:
		return "act outside the app"
	}
	return "read"
}

// LevelE is a rung of the capability ladder: the most powerful thing
// allowed.
type LevelE uint8

const (
	// LevelTalk: no window is shared; the model only answers.
	LevelTalk LevelE = iota
	// LevelRead: it reads what windows hold and show.
	LevelRead
	// LevelView: it changes what a window shows — a selection, a camera.
	LevelView
	// LevelEdit: it changes what a window holds — text, parameters.
	LevelEdit
	// LevelRun: it runs against a data source.
	LevelRun
	// LevelOutside: it acts outside the app — publishes, exports — with the
	// person's confirmation each time.
	LevelOutside
)

// AllLevels lists the ladder, lowest first.
var AllLevels = []LevelE{LevelTalk, LevelRead, LevelView, LevelEdit, LevelRun, LevelOutside}

func (inst LevelE) String() (s string) {
	switch inst {
	case LevelRead:
		return "read"
	case LevelView:
		return "change the view"
	case LevelEdit:
		return "edit"
	case LevelRun:
		return "run"
	case LevelOutside:
		return "act outside"
	}
	return "talk only"
}

// Level is the rung c reaches.
func (inst Ceiling) Level() (l LevelE) {
	c := inst.normal()
	switch {
	case c.Mode == ModeUnspecified:
		return LevelTalk
	case c.Effect >= app.OperationEffectConsequential:
		return LevelOutside
	case c.Effect == app.OperationEffectRun:
		return LevelRun
	case c.Effect == app.OperationEffectDocument:
		return LevelEdit
	case c.Effect == app.OperationEffectView:
		return LevelView
	}
	return LevelRead
}

// Score is where an allowance sits on the ladder.
type Score struct {
	// Level is the rung.
	Level LevelE
	// Position is the place on a scale of the whole ladder, in [0, 1]: each
	// rung is an equal band, and what else is allowed moves the position
	// within the rung's band.
	Position float64
	// Factors say, in words, what moved the position up within its band.
	Factors []string
}

// Value is the position as a whole number of 0 to 100.
func (inst Score) Value() (v int) { return int(inst.Position*100 + 0.5) }

// The weights of what moves a position within its band. They order the
// factors against each other and say nothing across bands: the band is the
// level's alone.
const (
	weightUnasked = 2 // changes apply without the person accepting each
	weightLaunch  = 1
	weightDesktop = 1
	weightData    = 1
	weightNetwork = 2
	weightRemote  = 1
	weightUnpaced = 2 // changes land faster than a person can follow
	// weightPixels is the most the model's sight of its captures adds, at
	// PixelsCaptures sent anywhere (ADR-0287 §SD6).
	weightPixels = 3
	weightAll    = weightUnasked + weightLaunch + weightDesktop + weightNetwork + weightRemote + weightUnpaced + weightPixels
	// bandInset keeps a position off its band's edges, so a marker always
	// reads as inside one band.
	bandInset = 0.12
)

// PixelsE is how much of its captures' pixels the model may see (ADR-0287):
// none, each one after the person allows that send, each one after the
// person allowed its content once, or every capture of the conversation.
// The chat enforces it; the ladder places it within the band, since seeing a
// granted window's pixels reads nothing the grant does not already reach.
type PixelsE uint8

const (
	PixelsNone PixelsE = iota
	PixelsAskEach
	PixelsAskOnce
	PixelsCaptures
)

var AllPixels = []PixelsE{PixelsNone, PixelsAskEach, PixelsAskOnce, PixelsCaptures}

func (inst PixelsE) String() string {
	switch inst {
	case PixelsAskEach:
		return "ask each time"
	case PixelsAskOnce:
		return "ask once per image"
	case PixelsCaptures:
		return "this chat's captures"
	}
	return "metadata only"
}

// Beside is what places a position besides the ceiling itself: where the
// model runs, and what of the captures it sees.
type Beside struct {
	// RemoteModel says the model is reached off this machine, which no
	// ceiling decides and every level is affected by.
	RemoteModel bool
	Pixels      PixelsE
	// PixelsLocalOnly keeps the pixels to a model on this machine or one
	// the host trusts with sealed data.
	PixelsLocalOnly bool
}

// Score places c on the ladder; remoteModel is Beside's.
func (inst Ceiling) Score(remoteModel bool) (s Score) {
	return inst.ScoreBeside(Beside{RemoteModel: remoteModel})
}

// ScoreBeside places c on the ladder with what lies beside it.
func (inst Ceiling) ScoreBeside(b Beside) (s Score) {
	c := inst.normal()
	s.Level = c.Level()
	w := 0
	add := func(on bool, weight int, factor string) {
		if on {
			w += weight
			s.Factors = append(s.Factors, factor)
		}
	}
	add(c.Mode == ModeAct && s.Level >= LevelView, weightUnasked, "changes apply without asking")
	add(c.Launch, weightLaunch, "opens windows")
	add(c.Desktop, weightDesktop, "arranges the desktop")
	add(c.Mode != ModeUnspecified && c.Reach == ReachData, weightData, "reaches data endpoints")
	add(c.Mode != ModeUnspecified && c.Reach == ReachNetwork, weightNetwork, "reaches the network")
	add(b.RemoteModel, weightRemote, "the model is off this machine")
	add(c.Unpaced && s.Level >= LevelView, weightUnpaced, "works faster than you can follow")
	if b.Pixels != PixelsNone {
		pw, factor := pixelsWeight(b.Pixels, b.PixelsLocalOnly)
		add(true, pw, factor)
	}
	within := bandInset + (1-2*bandInset)*float64(w)/float64(weightAll)
	s.Position = (float64(s.Level) + within) / float64(len(AllLevels))
	return
}

// pixelsWeight is what a pixels level adds and says: more the less the
// person decides each send, less when the pixels stay on a local model.
func pixelsWeight(p PixelsE, localOnly bool) (w int, factor string) {
	switch p {
	case PixelsAskEach:
		w, factor = 1, "sees a screenshot each time you allow it"
	case PixelsAskOnce:
		w, factor = 2, "sees a screenshot once you allowed it"
	default:
		w, factor = weightPixels, "sees this chat's screenshots"
	}
	if localOnly {
		w, factor = max(w-1, 1), factor+", on a local model only"
	}
	return
}

// Authority is a task's two bounds as the dispatcher holds them.
type Authority struct {
	// Limited says the coordinator set a ceiling; Ceiling is it.
	Limited bool
	Ceiling Ceiling
	// Granted is what the task's grant allows right now, under the ceiling.
	Granted Ceiling
}
