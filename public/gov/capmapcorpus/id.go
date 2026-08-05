package capmapcorpus

import (
	"path/filepath"
	"strings"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// naturalKeyBytes is the digest width for [NaturalKey]. Sixteen bytes matches
// what the runtime facts store already uses for its natural keys, so a
// capability key is the same shape as every other key in that table.
const naturalKeyBytes = 16

// naturalKeyDomain prefixes every digest so a capability key can never collide
// with a key minted for some other kind of fact from the same string.
const naturalKeyDomain = "capmap.capability"

// NormalizeSlug converts a raw slug to the corpus's canonical form, which is
// leeway's default naming style (LowerSpinalCase).
//
// This is what makes identity independent of how a name was typed:
// `AdversarialRobustness`, `adversarial_robustness`, `adversarialRobustness`
// and `adversarial-robustness` are one capability.
//
// It errors for anything that is not a well-formed name — notably any
// component beginning with a digit, which rejects citation keys like
// `Jouppi-1990` or `GDPR-Art-17`. That is not a defect to work around: such a
// string cannot be a capability slug, so a link naming one points outside the
// corpus. [NormalizeTarget] is the caller for that case.
func NormalizeSlug(raw string) (slug string, err error) {
	var sn naming.StylableName
	if sn, err = naming.MakeStylableName(strings.TrimSpace(raw)); err != nil {
		return "", eh.Errorf("unable to normalize slug %q: %w", raw, err)
	}
	return string(sn), nil
}

// NormalizeTarget normalizes a link target and reports whether it could be one
// at all.
//
// wellFormed false means the text is not a valid capability slug, so no
// capability can ever carry it — the link names something outside the corpus,
// such as a cited paper, a regulation, or a decision record. The raw text is
// returned unchanged in that case, because for a citation the raw text *is*
// the key.
//
// Callers must not treat a not-well-formed target as a broken link. On this
// repository's own capability catalog roughly a quarter of body links are
// citations of that kind, concentrated in the Standards and Obligations
// sections, and conflating the two makes a link check useless.
func NormalizeTarget(raw string) (target string, wellFormed bool) {
	trimmed := strings.TrimSpace(raw)
	slug, err := NormalizeSlug(trimmed)
	if err != nil {
		return trimmed, false
	}
	return slug, true
}

// NaturalKey derives a capability's durable identity from its normalized slug:
// a domain-separated blake3 digest, matching the natural-key convention the
// runtime facts store already follows.
//
// The slug is the whole of the identity. Moving a capability's file, renaming
// its display name, or re-scoring it does not change its key; only renaming
// the slug does, and that is a new capability by design.
//
// Pass a slug that has already been through [NormalizeSlug] — this function
// does not normalize, because silently accepting an unnormalized slug would
// mint two keys for one capability.
func NaturalKey(slug string) (key []byte) {
	h := blake3.New(naturalKeyBytes, nil)
	_, _ = h.Write([]byte(naturalKeyDomain))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(slug))
	return h.Sum(nil)
}

// slugForPath derives a capability's slug from where its file sits: the
// enclosing directory's name for a `capability.md`, the filename otherwise.
//
// A file whose slug will not normalize is an error rather than a skip. Every
// other capability is addressable and this one would not be, so the vault has
// a naming problem that a silent omission would hide.
func slugForPath(path string) (slug string, err error) {
	base := filepath.Base(path)
	raw := strings.TrimSuffix(base, ".md")
	if base == capabilityFileName {
		raw = filepath.Base(filepath.Dir(path))
	}
	if slug, err = NormalizeSlug(raw); err != nil {
		return "", eh.Errorf("unable to derive slug for %q: %w", path, err)
	}
	return slug, nil
}
