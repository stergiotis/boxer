package applink

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Codecs of launch kinds and alias schemes (ADR-0409 §SD3). An operation's
// codec rides on its catalog entry (app.OperationSpec.Link); a kind has no
// catalog, so its leaf registers the codec here, beside its kindcheck
// probe, at init.

var (
	codecsMu   sync.RWMutex
	kindCodecs = map[string]app.LinkCodecI{}
	aliases    = map[string]string{}
)

// reservedSchemes are the schemes no alias may take: the host's own and
// the ones another program owns.
var reservedSchemes = []string{Scheme, "http", "https", "mailto", "file"}

// RegisterKind registers the codec that spells a launch kind's config. A
// second codec for a kind is refused.
func RegisterKind(kind string, codec app.LinkCodecI) (err error) {
	if !validKind(kind) || codec == nil {
		return eb.Build().Str("kind", kind).Errorf("applink: a kind codec needs a kind name and a codec")
	}
	if err = CheckCodec(codec); err != nil {
		return eb.Build().Str("kind", kind).Errorf("applink: the kind's codec fails its own examples: %w", err)
	}
	codecsMu.Lock()
	defer codecsMu.Unlock()
	if _, dup := kindCodecs[kind]; dup {
		return eb.Build().Str("kind", kind).Errorf("applink: the kind already has a codec")
	}
	kindCodecs[kind] = codec
	return
}

// MustRegisterKind is RegisterKind for an init: a refusal is a wiring bug.
func MustRegisterKind(kind string, codec app.LinkCodecI) {
	if err := RegisterKind(kind, codec); err != nil {
		panic(err)
	}
}

// KindCodec is the codec a kind registered.
func KindCodec(kind string) (codec app.LinkCodecI, ok bool) {
	codecsMu.RLock()
	defer codecsMu.RUnlock()
	codec, ok = kindCodecs[kind]
	return
}

// Kinds lists the kinds with a codec, sorted.
func Kinds() (kinds []string) {
	codecsMu.RLock()
	defer codecsMu.RUnlock()
	for k := range kindCodecs {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	return
}

// RegisterAlias lets `<scheme>://<rest>` stand for
// `boxer:open/kind/<kind>/<rest>`: a readable name for a kind's links
// (`mdedit://`). The kind must have a codec; a second alias for a scheme,
// and the reserved schemes, are refused.
func RegisterAlias(scheme string, kind string) (err error) {
	if !validKind(scheme) || strings.ToLower(scheme) != scheme {
		return eb.Build().Str("scheme", scheme).Errorf("applink: an alias scheme is lower-case letters and digits")
	}
	for _, r := range reservedSchemes {
		if r == scheme {
			return eb.Build().Str("scheme", scheme).Errorf("applink: the scheme is reserved")
		}
	}
	codecsMu.Lock()
	defer codecsMu.Unlock()
	if _, ok := kindCodecs[kind]; !ok {
		return eb.Build().Str("scheme", scheme).Str("kind", kind).Errorf("applink: register the kind's codec before its alias")
	}
	if _, dup := aliases[scheme]; dup {
		return eb.Build().Str("scheme", scheme).Errorf("applink: the scheme already has an alias")
	}
	aliases[scheme] = kind
	return
}

// MustRegisterAlias is RegisterAlias for an init.
func MustRegisterAlias(scheme string, kind string) {
	if err := RegisterAlias(scheme, kind); err != nil {
		panic(err)
	}
}

// Aliases lists the alias schemes and their kinds, by scheme.
func Aliases() (schemes []string, kinds []string) {
	codecsMu.RLock()
	defer codecsMu.RUnlock()
	for s := range aliases {
		schemes = append(schemes, s)
	}
	slices.Sort(schemes)
	for _, s := range schemes {
		kinds = append(kinds, aliases[s])
	}
	return
}

func aliasKind(scheme string) (kind string, ok bool) {
	codecsMu.RLock()
	defer codecsMu.RUnlock()
	kind, ok = aliases[scheme]
	return
}

func resolveAlias(link string) (e Envelope, err error) {
	scheme, rest, ok := strings.Cut(link, "://")
	if !ok {
		err = eb.Build().Str("link", clip(link)).Errorf("applink: not a boxer: link")
		return
	}
	kind, ok := aliasKind(scheme)
	if !ok {
		err = eb.Build().Str("scheme", clip(scheme)).Errorf("applink: no alias is registered for the scheme")
		return
	}
	e = Envelope{Form: FormKind, Verb: VerbOpen, Kind: kind, Tail: "/" + rest}
	err = checkTail(e.Tail)
	return
}

// CheckParsed is what a fuzz test asks of a codec for any tail: reading
// it does not panic, and a tail it accepts formats to a link that reads
// back to the same arguments — one call, however it was spelled.
func CheckParsed(codec app.LinkCodecI, tail string) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = eb.Build().Str("tail", clip(tail)).Str("panic", fmt.Sprint(p)).Errorf("applink: the codec panicked")
		}
	}()
	args, perr := codec.ParseLink(tail)
	if perr != nil {
		return nil
	}
	again, ferr := codec.FormatLink(args)
	if ferr != nil {
		return eb.Build().Str("tail", clip(tail)).Errorf("applink: the codec accepts a tail it cannot spell: %w", ferr)
	}
	if cerr := checkTail(again); cerr != nil {
		return eb.Build().Str("tail", clip(again)).Errorf("applink: the codec spells a tail a link cannot carry: %w", cerr)
	}
	back, perr := codec.ParseLink(again)
	if perr != nil || !bytes.Equal(back, args) {
		return eb.Build().Str("tail", clip(tail)).Str("again", clip(again)).Errorf("applink: the codec's spelling reads back as other arguments")
	}
	return nil
}

// CheckCodec round-trips a codec's examples: each formats to a tail the
// envelope accepts, parses back to the same CBOR, and the parsed value
// formats to the same tail. It is what the conformance test runs over every
// registered codec; a codec that fails it spells a call two ways or reads
// back another.
func CheckCodec(codec app.LinkCodecI) (err error) {
	examples := codec.LinkExamples()
	if len(examples) == 0 {
		return eb.Build().Errorf("applink: a codec carries at least one example")
	}
	for i, ex := range examples {
		tail, ferr := codec.FormatLink(ex)
		if ferr != nil {
			return eb.Build().Int("example", i).Errorf("applink: format: %w", ferr)
		}
		if cerr := checkTail(tail); cerr != nil {
			return eb.Build().Int("example", i).Str("tail", clip(tail)).Errorf("applink: the tail is not one a link carries: %w", cerr)
		}
		back, perr := codec.ParseLink(tail)
		if perr != nil {
			return eb.Build().Int("example", i).Str("tail", clip(tail)).Errorf("applink: parse: %w", perr)
		}
		if !bytes.Equal(back, ex) {
			return eb.Build().Int("example", i).Str("tail", clip(tail)).Errorf("applink: the tail reads back to other arguments")
		}
		again, ferr := codec.FormatLink(back)
		if ferr != nil || again != tail {
			return eb.Build().Int("example", i).Str("tail", clip(tail)).Str("again", clip(again)).Errorf("applink: the arguments format to another tail the second time")
		}
	}
	return
}

// Examples are an operation's codec examples spelled as links that open a
// window of the app: how a reader, or a model, learns the app's spelling
// (ADR-0409 (proposed) §SD3). An example the codec cannot format is left
// out.
func Examples(m app.Manifest, o app.OperationSpec) (links []string) {
	if o.Link == nil {
		return
	}
	for _, ex := range o.Link.LinkExamples() {
		tail, err := o.Link.FormatLink(ex)
		if err != nil {
			continue
		}
		links = append(links, Format(Envelope{Form: FormApp, Verb: VerbOpen, App: m.SubjectAlias(), Operation: o.Name, Version: o.Version, Tail: tail}))
	}
	return
}
