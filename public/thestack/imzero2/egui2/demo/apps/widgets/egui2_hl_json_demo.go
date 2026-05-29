//go:build llm_generated_opus47

package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// Pre-built retained jobs for static JSON snippets — zero per-frame cost on
// the Go side. Each PrepareJson call drives encoding/json/jsontext
// once at init() and produces a retained CodeViewJob that the FFFI client
// emits as reference each frame.
var (
	jsonSimple = codeview.PrepareJson(`{"name":"alice","age":30,"active":true}`)

	jsonPretty = codeview.PrepareJson(`{
  "users": [
    {"id": 1, "name": "alice", "admin": true},
    {"id": 2, "name": "bob", "admin": false}
  ],
  "page": 1,
  "next": null
}`)

	jsonNested = codeview.PrepareJson(`{
  "request": {
    "method": "POST",
    "headers": {
      "content-type": "application/json",
      "x-trace-id": "abc-123"
    },
    "body": {
      "events": [
        {"type": "click", "ts": 1714291200, "value": 0.5},
        {"type": "view",  "ts": 1714291201, "value": 1.0}
      ]
    }
  }
}`)

	jsonNumeric = codeview.PrepareJson(`{
  "int": 42,
  "neg": -7,
  "float": 3.14159,
  "exp": 1.5e-10,
  "big": 1.7976931348623157e+308,
  "zero": 0,
  "ints": [1, 2, 3, 4, 5]
}`)

	// jsonMalformed shows graceful degradation: the prefix decodes cleanly
	// (keys + values syntax-highlighted) while the unparseable tail falls
	// through to CategoryPlain (default color).
	jsonMalformed = codeview.PrepareJson(`{"k":"v",garbage_after_comma`)
)

func demoJsonView(ids *c.WidgetIdStack) {
	for range c.CollapsingHeader(ids.PrepareStr("json-simple"), c.WidgetText().Text("simple object").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-json-simple"), jsonSimple).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("json-pretty"), c.WidgetText().Text("pretty-printed (array of objects + null)").Keep()).DefaultOpen(true).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-json-pretty"), jsonPretty).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("json-nested"), c.WidgetText().Text("deeply nested request envelope").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-json-nested"), jsonNested).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("json-numeric"), c.WidgetText().Text("number forms").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-json-numeric"), jsonNumeric).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("json-malformed"), c.WidgetText().Text("malformed (graceful fallback)").Keep()).KeepIter() {
		c.CodeView(ids.PrepareStr("cv-json-malformed"), jsonMalformed).Send()
	}
}
