package tables

import (
	"bufio"
	"bytes"
	"embed"
	"strconv"
	"strings"
	"sync"
)

//go:embed wmo/*.tsv wmo/VERSION
var wmoFS embed.FS

// Entry is one row of a code table: the meaning the WMO gives a value, its
// unit where the table has one (parameter tables), and its status
// (Operational, Deprecated, …). A row may cover a range of values.
type Entry struct {
	Lo, Hi  uint32
	Meaning string
	Unit    string
	Status  string
}

// Octet is one row of a template layout: the octets it occupies (as the
// WMO writes them, "12-13"), their count, the contents, and the code or
// flag table the value is looked up in, if any.
type Octet struct {
	Octets    string
	Count     int
	Contents  string
	CodeTable string
	FlagTable string
	Status    string
}

type data struct {
	version   string
	codes     map[string][]Entry // "4.5", "4.2.0.0"
	flags     map[string][]flagEntry
	templates map[string][]Octet // "4.40"
	centres   map[uint32]Entry
}

type flagEntry struct {
	bit     uint8
	value   uint8
	meaning string
	status  string
}

var load = sync.OnceValue(func() (d *data) {
	d = &data{codes: map[string][]Entry{}, flags: map[string][]flagEntry{}, templates: map[string][]Octet{}, centres: map[uint32]Entry{}}
	if v, err := wmoFS.ReadFile("wmo/VERSION"); err == nil {
		d.version = strings.TrimSpace(string(v))
	}
	if b, err := wmoFS.ReadFile("wmo/codes.tsv"); err == nil {
		for _, f := range rows(b) {
			if len(f) < 6 {
				continue
			}
			lo, _ := strconv.ParseUint(f[1], 10, 32)
			hi, _ := strconv.ParseUint(f[2], 10, 32)
			d.codes[f[0]] = append(d.codes[f[0]], Entry{Lo: uint32(lo), Hi: uint32(hi), Meaning: f[3], Unit: f[4], Status: f[5]})
		}
	}
	if b, err := wmoFS.ReadFile("wmo/flags.tsv"); err == nil {
		for _, f := range rows(b) {
			if len(f) < 5 {
				continue
			}
			bit, _ := strconv.ParseUint(f[1], 10, 8)
			val, _ := strconv.ParseUint(f[2], 10, 8)
			d.flags[f[0]] = append(d.flags[f[0]], flagEntry{bit: uint8(bit), value: uint8(val), meaning: f[3], status: f[4]})
		}
	}
	if b, err := wmoFS.ReadFile("wmo/templates.tsv"); err == nil {
		for _, f := range rows(b) {
			if len(f) < 7 {
				continue
			}
			n, _ := strconv.Atoi(f[2])
			d.templates[f[0]] = append(d.templates[f[0]], Octet{Octets: f[1], Count: n, Contents: f[3], CodeTable: f[4], FlagTable: f[5], Status: f[6]})
		}
	}
	if b, err := wmoFS.ReadFile("wmo/centres.tsv"); err == nil {
		for _, f := range rows(b) {
			if len(f) < 3 {
				continue
			}
			code, err := strconv.ParseUint(f[0], 10, 32)
			if err != nil {
				continue
			}
			d.centres[uint32(code)] = Entry{Lo: uint32(code), Hi: uint32(code), Meaning: f[1], Status: f[2]}
		}
	}
	return
})

// rows splits a generated TSV into fields, skipping comment lines.
func rows(b []byte) (out [][]string) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Split(line, "\t"))
	}
	return
}

// Version is the GRIB2 master table version the embedded tables came from,
// as the source repository tags it ("v37").
func Version() (v string) {
	v = load().version
	return
}

// Code looks a value up in a code table named the WMO way — "0.0", "4.5",
// "5.0" — and returns its row when the table has one, including range rows.
func Code(table string, value uint32) (e Entry, ok bool) {
	for _, row := range load().codes[table] {
		if value >= row.Lo && value <= row.Hi {
			e = row
			ok = true
			return
		}
	}
	return
}

// Parameter looks a parameter triplet up in code table 4.2. Numbers in
// the local range (192–254) and the missing value are never named, whatever
// the table says for the range (ADR-0292 §R8).
func Parameter(discipline, category, number uint8) (e Entry, ok bool) {
	if number >= 192 || category >= 192 || discipline >= 192 {
		return
	}
	e, ok = Code("4.2."+strconv.Itoa(int(discipline))+"."+strconv.Itoa(int(category)), uint32(number))
	if ok && strings.HasPrefix(e.Meaning, "Reserved") {
		ok = false
	}
	return
}

// Flag returns the meaning of one bit's value in a flag table ("3.3",
// "3.4"), bits numbered from 1 at the most significant end as the WMO
// numbers them.
func Flag(table string, bit uint8, value uint8) (meaning string, ok bool) {
	for _, row := range load().flags[table] {
		if row.bit == bit && row.value == value {
			meaning = row.meaning
			ok = true
			return
		}
	}
	return
}

// Template returns the octet layout of a template ("3.0", "4.40", "5.42")
// in the WMO's order, or nil.
func Template(section uint8, number uint16) (layout []Octet) {
	layout = load().templates[strconv.Itoa(int(section))+"."+strconv.Itoa(int(number))]
	return
}

// Centre names an originating centre from common code table C-11. The
// missing value (all ones) is not a centre and is never named.
func Centre(code uint16) (name string, ok bool) {
	if code == 0xffff {
		return
	}
	e, ok := load().centres[uint32(code)]
	name = e.Meaning
	return
}
