package eh

import (
	"github.com/fxamacker/cbor/v2"
)

// LookupFact returns the value stored under key in the structured data of
// err or of an error it wraps (the facts eb.Build attaches), decoded into
// T. The chain is searched outside in, depth first through joined errors,
// and the first error that carries key decides: ok is false when none
// does, or when that value does not decode into T.
func LookupFact[T any](err error, key string) (v T, ok bool) {
	raw, found := lookupRaw(err, key)
	if !found {
		return
	}
	if cbor.Unmarshal(raw, &v) != nil {
		var zero T
		return zero, false
	}
	return v, true
}

func lookupRaw(err error, key string) (raw cbor.RawMessage, found bool) {
	if err == nil {
		return
	}
	if esd, isData := err.(ErrorWithStructuredDataI); isData {
		if data := esd.GetCBORStructuredData(); len(data) > 0 {
			var facts map[string]cbor.RawMessage
			if cbor.Unmarshal(data, &facts) == nil {
				raw, found = facts[key]
				if found {
					return
				}
			}
		}
	}
	switch et := err.(type) {
	case unwrapableMulti:
		for _, child := range et.Unwrap() {
			if child != nil && child != err {
				raw, found = lookupRaw(child, key)
				if found {
					return
				}
			}
		}
	case unwrapableSingle:
		if child := et.Unwrap(); child != nil && child != err {
			raw, found = lookupRaw(child, key)
		}
	}
	return
}
