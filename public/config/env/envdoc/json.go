package envdoc

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// JSONSchema names the shape RenderJSON emits (ADR-0009 §4). A consumer
// checks it before reading the variables; it changes when a field's
// meaning does, not when variables are added.
const JSONSchema = "boxer.env-vars/1"

type jsonOrigin struct {
	Module  string `json:"module"`
	Package string `json:"package"`
}

type jsonVariable struct {
	Name        string        `json:"name"`
	Type        env.TypeE     `json:"type"`
	Default     string        `json:"default,omitempty"`
	Description string        `json:"description"`
	Category    env.CategoryE `json:"category"`
	Sensitive   bool          `json:"sensitive"`
	CliFlagName string        `json:"cliFlagName,omitempty"`
	Allowed     []string      `json:"allowed,omitempty"`
	Origin      jsonOrigin    `json:"origin"`
}

type jsonDocument struct {
	Schema    string         `json:"schema"`
	Variables []jsonVariable `json:"variables"`
}

// RenderJSON renders env.Snapshot() as the machine-readable form of the
// markdown reference (ADR-0009 §4): the Spec fields of every linked
// variable, sorted by Name, with no timestamp, so two renders of one
// registry are byte-identical and a committed copy can be pinned to the
// registry by a test. An empty Default is omitted; the registry reads
// empty as unset.
func RenderJSON() (out []byte, err error) {
	specs := env.Snapshot()
	doc := jsonDocument{
		Schema:    JSONSchema,
		Variables: make([]jsonVariable, 0, len(specs)),
	}
	for _, s := range specs {
		doc.Variables = append(doc.Variables, jsonVariable{
			Name:        s.Name,
			Type:        s.Type,
			Default:     s.Default,
			Description: s.Description,
			Category:    s.Category,
			Sensitive:   s.Sensitive,
			CliFlagName: s.CliFlagName,
			Allowed:     s.Allowed,
			Origin: jsonOrigin{
				Module:  s.Origin.Module,
				Package: s.Origin.Package,
			},
		})
	}
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf,
		jsontext.EscapeForHTML(false),
		jsontext.EscapeForJS(false),
		jsontext.Multiline(true),
		jsontext.WithIndent("  "),
	)
	err = json.MarshalEncode(enc, doc)
	if err != nil {
		err = eb.Build().Errorf("encode env-var snapshot: %w", err)
		return
	}
	out = buf.Bytes()
	return
}
