package repo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Mailmap canonicalizes git author and co-author identities per the gitmailmap
// format (https://git-scm.com/docs/gitmailmap): one canonical identity per
// line, four supported forms. A nil Mailmap resolves every identity to itself
// (email lower-cased), so analyzers can call Resolve unconditionally and stay
// on the pre-mailmap code path when no .mailmap is present.
type Mailmap struct {
	// byPair matches a (commit email, commit name) pair — form 4, the
	// disambiguation case where two people share an email and only a specific
	// name should fold into the canonical identity. Highest precedence.
	byPair map[mailmapKey]mailmapEntry
	// byEmail matches a commit email alone — forms 1, 2, 3.
	byEmail map[string]mailmapEntry
}

type mailmapKey struct {
	email string // lower-cased commit email
	name  string // commit name, case-sensitive (form 4 disambiguation)
}

// mailmapEntry is one resolved canonical identity: canonName empty means keep
// the original commit name (form 2); canonEmail is always lower-cased.
type mailmapEntry struct {
	canonName  string
	canonEmail string
}

// Resolve maps (name, email) to its canonical (name, email). A name+email pair
// entry (form 4) wins over an email-only entry (forms 1-3); email matching is
// case-insensitive; form-4 name matching is case-sensitive so a shared email
// only folds the named identity. A nil receiver leaves the name untouched and
// lower-cases the email — the identity git emitted, minimally normalized.
func (m *Mailmap) Resolve(name, email string) (canonName, canonEmail string) {
	if m == nil {
		return name, strings.ToLower(email)
	}
	lc := strings.ToLower(email)
	if e, ok := m.byPair[mailmapKey{email: lc, name: name}]; ok {
		return applyMailmapEntry(e, name)
	}
	if e, ok := m.byEmail[lc]; ok {
		return applyMailmapEntry(e, name)
	}
	return name, lc
}

// applyMailmapEntry fills an entry's canonical name/email, keeping the original
// name where the entry leaves it unset (form 2 keeps the commit name).
func applyMailmapEntry(e mailmapEntry, origName string) (canonName, canonEmail string) {
	canonEmail = e.canonEmail
	canonName = origName
	if e.canonName != "" {
		canonName = e.canonName
	}
	return
}

// ParseMailmap parses gitmailmap text into a Mailmap. Blank lines and lines
// beginning with '#' are ignored; malformed lines (no <email> or an unmatched
// '<') are skipped rather than erroring, mirroring git's tolerance. Returns nil
// when no usable entry is found, so the caller's nil-receiver Resolve fast path
// applies and no allocation lingers for mailmap-less repositories.
func ParseMailmap(content string) *Mailmap {
	m := &Mailmap{
		byPair:  make(map[mailmapKey]mailmapEntry),
		byEmail: make(map[string]mailmapEntry),
	}
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, ok := parseMailmapLine(line)
		if !ok {
			continue
		}
		entry := mailmapEntry{canonName: p.canonName, canonEmail: p.canonEmail}
		if p.commitName != "" {
			// form 4: pair only — does not also merge other names on this email.
			m.byPair[mailmapKey{email: p.commitEmail, name: p.commitName}] = entry
		} else {
			// forms 1-3: email match. Form 1's commit email equals its canonical
			// email, so it lands here as a name fix for an already-canonical email.
			m.byEmail[p.commitEmail] = entry
		}
	}
	if len(m.byPair) == 0 && len(m.byEmail) == 0 {
		return nil
	}
	return m
}

// parsedMailmapLine is one mailmap entry before it is routed into byPair/byEmail.
type parsedMailmapLine struct {
	canonName   string
	canonEmail  string // lower-cased
	commitName  string // empty for forms 1-3; set for form 4
	commitEmail string // lower-cased; equals canonEmail for form 1
}

// parseMailmapLine recognizes the four gitmailmap forms by their <email>
// segments:
//
//	Proper Name <email>                              // form 1: name fix
//	<proper@email> <commit@email>                   // form 2: email merge
//	Proper Name <proper@email> <commit@email>        // form 3: email + name merge
//	Proper Name <proper@email> Commit Name <commit>  // form 4: pair disambiguation
func parseMailmapLine(line string) (p parsedMailmapLine, ok bool) {
	br := mailmapBrackets(line)
	if len(br) < 1 {
		return
	}
	if len(br) == 1 {
		// form 1: the single email is both the commit and canonical email.
		email := strings.ToLower(br[0].email)
		if email == "" {
			return
		}
		p.canonName = strings.TrimSpace(line[:br[0].openIdx])
		p.canonEmail = email
		p.commitEmail = email
		ok = true
		return
	}
	// forms 2-4: first <...> is proper email, second is commit email.
	properEmail := strings.ToLower(br[0].email)
	commitEmail := strings.ToLower(br[1].email)
	if properEmail == "" || commitEmail == "" {
		return
	}
	p.canonName = strings.TrimSpace(line[:br[0].openIdx])
	p.canonEmail = properEmail
	p.commitName = strings.TrimSpace(line[br[0].closeIdx+1 : br[1].openIdx])
	p.commitEmail = commitEmail
	ok = true
	return
}

// mailmapBracket is one <email> segment with its delimiter offsets.
type mailmapBracket struct {
	email    string
	openIdx  int
	closeIdx int
}

// mailmapBrackets scans a line left to right for <...> email segments, stopping
// at an unmatched '<' (malformed) so a stray angle bracket cannot misparse a
// later well-formed segment.
func mailmapBrackets(line string) []mailmapBracket {
	var out []mailmapBracket
	for i := 0; i < len(line); i++ {
		if line[i] != '<' {
			continue
		}
		j := strings.IndexByte(line[i+1:], '>')
		if j < 0 {
			return out
		}
		closeIdx := i + 1 + j
		out = append(out, mailmapBracket{
			email:    strings.TrimSpace(line[i+1 : closeIdx]),
			openIdx:  i,
			closeIdx: closeIdx,
		})
		i = closeIdx
	}
	return out
}

// LoadMailmap builds a Mailmap from the repository's .mailmap file — git's
// default identity-canonicalization source. It resolves the working tree's top
// level (git rev-parse --show-toplevel) and reads .mailmap there, so a --repo
// pointing anywhere inside the tree still finds the same file git itself would,
// and the git-reported path sidesteps symlink mismatches. A missing .mailmap is
// not an error: it returns nil, and the analyzers' nil-receiver Resolve keeps
// the pre-mailmap behaviour (email lower-cased). Failure to locate a work tree
// (bare repo, git unavailable) likewise yields nil rather than an error — the
// downstream git commands surface any real "not a repository" failure with a
// clearer message. Only a present-but-unreadable .mailmap is returned as an
// error, so an intended canonicalization is never silently dropped.
func LoadMailmap(ctx context.Context, git *GitRunner) (mm *Mailmap, err error) {
	var toplevel string
	for line, iterErr := range git.RunLines(ctx, "rev-parse", "--show-toplevel") {
		if iterErr != nil {
			return nil, nil
		}
		if toplevel == "" {
			toplevel = strings.TrimSpace(line)
		}
	}
	if toplevel == "" {
		return nil, nil
	}
	content, readErr := os.ReadFile(filepath.Join(toplevel, ".mailmap"))
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, eh.Errorf("unable to read .mailmap at %q: %w", toplevel, readErr)
	}
	return ParseMailmap(string(content)), nil
}
