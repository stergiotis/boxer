package doclint

import (
	"gopkg.in/yaml.v3"
)

// mdFrontMatter aggregates the front-matter fields used by Markdown rules.
// New rules that need additional fields should add them here; rules ignore
// fields they do not consume, so growth is non-breaking.
type mdFrontMatter struct {
	Type         string `yaml:"type"`
	Audience     string `yaml:"audience,omitempty"`
	Status       string `yaml:"status"`
	ReviewedBy   string `yaml:"reviewed-by,omitempty"`
	ReviewedDate string `yaml:"reviewed-date,omitempty"`
}

// parseMdFrontMatter reads the leading YAML stanza from data and returns the
// parsed metadata, the body bytes, and whether the stanza was successfully
// extracted. Used by every Markdown rule that needs front-matter context.
//
// A non-nil err means YAML parse failure. ok=false means no stanza was
// present at all (the file lacks front-matter); rules that depend on the
// stanza should treat both ok=false and err!=nil as "out of scope for this
// rule" because DL001 owns the stanza-presence and parse-validity contract.
func parseMdFrontMatter(data []byte) (meta mdFrontMatter, body []byte, ok bool, err error) {
	var fm []byte
	fm, body, ok = ExtractFrontMatter(data)
	if !ok {
		return
	}
	err = yaml.Unmarshal(fm, &meta)
	return
}
