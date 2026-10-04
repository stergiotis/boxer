package chat

// The artefact (ADR-0282): one markdown document per conversation, which
// the model reads and edits through the artefact_* tools and the person
// reviews in a panel. This file is the document and its revisions; the tools
// are in chat_artefact_tools.go and the panel in chat_artefact_render.go.
//
// The artefact is shared by two goroutines: the turn's, whose tool calls
// write it, and the render goroutine, which draws it and reverts. Every
// method takes the lock; nothing here touches the UI.

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ArtefactSeed turns the Artefact option on in a new window (ADR-0009 seed
// variable).
var ArtefactSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_ARTEFACT",
	Default:     "false",
	Description: "turn on the Artefact in a new chat window: one markdown document per conversation that the model edits through tools (ADR-0282); for scenes and demos",
	Category:    env.CategoryE("boxer-chat"),
})

// revSourceE is what made a revision.
type revSourceE uint8

const (
	revSourceModel revSourceE = iota
	// revSourceRevert is the person making an earlier revision current.
	revSourceRevert
	// revSourcePerson is the person removing a screenshot in the panel.
	revSourcePerson
)

// artRevision is one state of the document. Revision 0 is the empty
// document every artefact starts as; it is implicit.
type artRevision struct {
	n      int
	text   string
	source revSourceE
	// turn and tool are the turn and tool that made a model revision; title
	// is the call's title, what the person reads in the list.
	turn  string
	tool  string
	title string
	// revertedTo is the revision a revert made current again.
	revertedTo int
	// changed is the lines the change occupies in this revision.
	changedFirst int
	changedLast  int
	atMs         int64
	// images is the screenshot set of this revision (ADR-0284 §SD2). A
	// commit with ownImages false takes the head's: a text write leaves the
	// set as it was.
	images    []artImage
	ownImages bool
	// imageNote says what a change of the set did, e.g. "+ screenshot-1.png".
	imageNote string
}

// artPolicy is what the settings let the model do to the artefact
// (ADR-0282 §SD3): write at all, and whether a write waits for the person.
type artPolicy struct {
	write bool
	ask   bool
}

// artProposal is a write waiting for the person under Ask first.
type artProposal struct {
	seq   uint64
	base  int
	text  string
	tool  string
	title string
	// images, for a change of the screenshot set, is the set it proposes,
	// and image the entry it adds or removes, for the panel to show.
	images    []artImage
	ownImages bool
	image     *artImage
	removes   bool
	// reply carries the verdict to the waiting tool call; decided keeps a
	// second click from sending twice. decided is the render goroutine's.
	reply   chan bool
	decided bool
}

// artefact is the document of one conversation.
type artefact struct {
	mu       sync.Mutex
	revs     []artRevision
	proposal *artProposal
	seq      uint64
	// lastN is the highest revision number handed out; a number taken back
	// by a rewind is not handed out again.
	lastN  int
	policy artPolicy
	meta   artMeta
	// store holds the screenshots' bytes (ADR-0284).
	store *imageStore
}

func newArtefact() *artefact { return &artefact{store: newImageStore(imageLimitsNow())} }

// close frees the screenshots; the conversation is over.
func (inst *artefact) close() { inst.store.close() }

// headImages is a copy of the current revision's screenshot set.
func (inst *artefact) headImages() (out []artImage) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(out, inst.headImagesLocked()...)
}

func (inst *artefact) headImagesLocked() []artImage {
	if len(inst.revs) == 0 {
		return nil
	}
	return inst.revs[len(inst.revs)-1].images
}

// gcLocked frees the bytes no revision and no waiting proposal names. It
// runs where the head moves: after that, a rewound turn's revisions can no
// longer be reinstated (reinstate needs the head where truncate left it).
func (inst *artefact) gcLocked() {
	keep := make(map[string]bool)
	for _, r := range inst.revs {
		for _, e := range r.images {
			keep[e.hash] = true
		}
	}
	if p := inst.proposal; p != nil {
		for _, e := range p.images {
			keep[e.hash] = true
		}
	}
	inst.store.retain(keep)
}

// releaseUnreferenced frees what a rejected or withdrawn proposal sealed.
func (inst *artefact) releaseUnreferenced() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.gcLocked()
}

// purge frees a hash's bytes and marks every entry naming it, in every
// revision, as purged (ADR-0284 §SD2).
func (inst *artefact) purge(hash string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for i := range inst.revs {
		imgs := inst.revs[i].images
		for j := range imgs {
			if imgs[j].hash == hash && !imgs[j].purged {
				// Revisions share no backing arrays (commit copies), so the
				// mark lands in this revision only.
				imgs[j].purged = true
			}
		}
	}
	inst.store.purge(hash)
}

// removeImage is the person removing a screenshot in the panel, as a
// revision of its own; purge frees the bytes too. Like a revert, it waits
// for no proposal: one that is waiting is decided first.
func (inst *artefact) removeImage(name string, purge bool) (n int, err error) {
	inst.mu.Lock()
	if inst.proposal != nil {
		inst.mu.Unlock()
		return 0, eb.Build().Errorf("a proposed change is waiting: accept or reject it first")
	}
	set := inst.headImagesLocked()
	i := findImage(set, name)
	if i < 0 {
		inst.mu.Unlock()
		return 0, eb.Build().Str("name", name).Errorf("no such screenshot")
	}
	e := set[i]
	if purge {
		for j, o := range set {
			if j != i && o.hash == e.hash && !o.purged {
				inst.mu.Unlock()
				return 0, errSharedBytes{other: o.name}
			}
		}
	}
	next := append(append([]artImage(nil), set[:i]...), set[i+1:]...)
	_, text := inst.headLocked()
	n = inst.nextLocked()
	note := "− " + name
	if purge {
		note += ", purged"
	}
	inst.revs = append(inst.revs, artRevision{n: n, text: text, source: revSourcePerson, atMs: time.Now().UnixMilli(),
		images: next, ownImages: true, imageNote: note})
	inst.gcLocked()
	inst.mu.Unlock()
	if purge {
		inst.purge(e.hash)
	}
	return
}

func (inst *artefact) setMeta(m artMeta) {
	inst.mu.Lock()
	inst.meta = m
	inst.mu.Unlock()
}

func (inst *artefact) metaNow() (m artMeta) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.meta
}

// head is the current revision's number and text.
func (inst *artefact) head() (n int, text string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.headLocked()
}

func (inst *artefact) headLocked() (n int, text string) {
	if len(inst.revs) == 0 {
		return 0, ""
	}
	r := inst.revs[len(inst.revs)-1]
	return r.n, r.text
}

// revisions is a copy of the revision list, oldest first.
func (inst *artefact) revisions() (out []artRevision) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(out, inst.revs...)
}

// text is revision n's text.
func (inst *artefact) text(n int) (s string, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.textLocked(n)
}

func (inst *artefact) textLocked(n int) (s string, ok bool) {
	if n == 0 {
		return "", true
	}
	for _, r := range inst.revs {
		if r.n == n {
			return r.text, true
		}
	}
	return "", false
}

func (inst *artefact) setPolicy(p artPolicy) {
	inst.mu.Lock()
	inst.policy = p
	inst.mu.Unlock()
}

func (inst *artefact) policyNow() (p artPolicy) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.policy
}

// errStale is a write whose base is no longer the head.
type errStale struct{ base, head int }

func (inst errStale) Error() string {
	return "the artefact is at revision " + strconv.Itoa(inst.head) + ", not " + strconv.Itoa(inst.base) + ": read it again and redo the change"
}

// commit makes text the next revision when base is still the head.
func (inst *artefact) commit(base int, rev artRevision) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	head, _ := inst.headLocked()
	if base != head {
		err = errStale{base: base, head: head}
		return
	}
	if rev.ownImages {
		rev.images = append([]artImage(nil), rev.images...)
	} else {
		rev.images = append([]artImage(nil), inst.headImagesLocked()...)
	}
	rev.n = inst.nextLocked()
	if rev.atMs == 0 {
		rev.atMs = time.Now().UnixMilli()
	}
	inst.revs = append(inst.revs, rev)
	inst.gcLocked()
	return rev.n, nil
}

// nextLocked is the next revision's number.
func (inst *artefact) nextLocked() int {
	inst.lastN++
	return inst.lastN
}

// revert makes revision to current again, as a new revision.
func (inst *artefact) revert(to int) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.proposal != nil {
		err = eb.Build().Int("to", to).Errorf("a proposed change is waiting: accept or reject it first")
		return
	}
	text, ok := inst.textLocked(to)
	if !ok {
		err = eb.Build().Int("to", to).Errorf("no such revision")
		return
	}
	head, _ := inst.headLocked()
	if head == to {
		return head, nil
	}
	n = inst.nextLocked()
	inst.revs = append(inst.revs, artRevision{n: n, text: text, source: revSourceRevert, revertedTo: to, atMs: time.Now().UnixMilli(),
		images: append([]artImage(nil), inst.imagesLocked(to)...), ownImages: true})
	inst.gcLocked()
	return
}

// imagesLocked is revision n's screenshot set.
func (inst *artefact) imagesLocked(n int) []artImage {
	for _, r := range inst.revs {
		if r.n == n {
			return r.images
		}
	}
	return nil
}

// truncate takes back every revision made after revision n and returns
// them, for reinstate. A rewound turn takes its revisions with it.
func (inst *artefact) truncate(n int) (dropped []artRevision) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for i, r := range inst.revs {
		if r.n > n {
			dropped = append(dropped, inst.revs[i:]...)
			inst.revs = inst.revs[:i:i]
			return
		}
	}
	return
}

// reinstate puts back what truncate took, when the head is still at where
// truncate left it; a revert since then keeps it from landing.
func (inst *artefact) reinstate(at int, dropped []artRevision) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if head, _ := inst.headLocked(); head != at {
		return false
	}
	inst.revs = append(inst.revs, dropped...)
	return true
}

// propose shows a write to the person and waits for the verdict, or for
// the turn to end.
func (inst *artefact) propose(ctx context.Context, p *artProposal) (accepted bool, err error) {
	p.reply = make(chan bool, 1)
	inst.mu.Lock()
	if inst.proposal != nil {
		inst.mu.Unlock()
		err = eb.Build().Errorf("another change is waiting for the person")
		return
	}
	inst.seq++
	p.seq = inst.seq
	inst.proposal = p
	inst.mu.Unlock()
	defer func() {
		inst.mu.Lock()
		if inst.proposal == p {
			inst.proposal = nil
		}
		inst.mu.Unlock()
	}()
	select {
	case accepted = <-p.reply:
	case <-ctx.Done():
		err = ctx.Err()
	}
	return
}

// pending is the proposal waiting for the person, nil when none is.
func (inst *artefact) pending() (p *artProposal) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.proposal
}

// decide answers a proposal; the render goroutine calls it once per click.
func (inst *artProposal) decide(accept bool) {
	if inst.decided {
		return
	}
	inst.decided = true
	inst.reply <- accept
}
