package logging

import (
	"bytes"
	"encoding/base64"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

const embeddedCborPrfixStr = "\"data:application/cbor;base64,"

var embeddedCborPrefix = []byte(embeddedCborPrfixStr)

func containsEmbeddedCborJson(s string) bool {
	return strings.HasPrefix(s, embeddedCborPrfixStr) && len(s) > len(embeddedCborPrefix)+2
}
func containsEmbeddedCbor(s string) bool {
	return strings.HasPrefix(s, embeddedCborPrfixStr[1:]) && len(s) > len(embeddedCborPrefix)
}
func embeddAsCbor(cborEncMode cbor.EncMode, v any) (s string, err error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	err = cborEncMode.NewEncoder(buf).Encode(v)
	if err == nil {
		b := buf.Bytes()
		l := base64.StdEncoding.EncodedLen(len(b))
		r := make([]byte, l+len(embeddedCborPrefix)+1)
		copy(r, embeddedCborPrefix)
		r[len(r)-1] = '"'
		base64.StdEncoding.Encode(r[len(embeddedCborPrefix):], b)
		return string(r), nil
	}
	buf.Reset()
	err = json.MarshalEncode(jsontext.NewEncoder(buf,
		jsontext.EscapeForHTML(false),
		jsontext.EscapeForJS(false),
	), v,
		json.DefaultOptionsV2())
	if err != nil {
		return
	}
	s = buf.String()
	return
}
func unpackEmbeddedCborJson(s string) (r []byte, err error) {
	b := s[len(embeddedCborPrefix) : len(s)-1]
	r, err = base64.StdEncoding.DecodeString(b)
	if err != nil {
		r = nil
		return
	}
	return
}
func unpackEmbeddedCbor(s string) (r []byte, err error) {
	b := s[len(embeddedCborPrefix)-1:]
	r, err = base64.StdEncoding.DecodeString(b)
	if err != nil {
		r = nil
		return
	}
	return
}
func unmarshallEmbeddedCbor(s string, cborDecMode cbor.DecMode, v any) (err error) {
	var r []byte
	r, err = unpackEmbeddedCbor(s)
	if err != nil {
		return
	}
	err = cborDecMode.Unmarshal(r, v)
	return
}
func unmarshallEmbeddedCborJson(s string, cborDecMode cbor.DecMode, v any) (err error) {
	var r []byte
	r, err = unpackEmbeddedCborJson(s)
	if err != nil {
		return
	}
	err = cborDecMode.Unmarshal(r, v)
	return
}
