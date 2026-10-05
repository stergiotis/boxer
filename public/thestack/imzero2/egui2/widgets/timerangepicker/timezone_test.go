package timerangepicker

import (
	"os"
	"strings"
	"testing"
)

func TestLookupReservedIDs(t *testing.T) {
	cat := newTzCatalogue()
	id, err := cat.lookup("System")
	if err != nil {
		t.Fatalf("lookup System: %v", err)
	}
	if id != TzIDSystem {
		t.Errorf("System TzID: want %d, got %d", TzIDSystem, id)
	}
	id, err = cat.lookup("UTC")
	if err != nil {
		t.Fatalf("lookup UTC: %v", err)
	}
	if id != TzIDUTC {
		t.Errorf("UTC TzID: want %d, got %d", TzIDUTC, id)
	}
}

func TestLookupAssignsStableIDs(t *testing.T) {
	cat := newTzCatalogue()
	tokyoA, err := cat.lookup("Asia/Tokyo")
	if err != nil {
		t.Fatalf("lookup Asia/Tokyo: %v", err)
	}
	tokyoB, err := cat.lookup("Asia/Tokyo")
	if err != nil {
		t.Fatalf("lookup Asia/Tokyo (re-fetch): %v", err)
	}
	if tokyoA != tokyoB {
		t.Errorf("TzID should be stable: got %d then %d", tokyoA, tokyoB)
	}
	if tokyoA == TzIDSystem || tokyoA == TzIDUTC {
		t.Errorf("Asia/Tokyo should not collide with reserved ids; got %d", tokyoA)
	}
}

func TestLookupRejectsUnknownZone(t *testing.T) {
	cat := newTzCatalogue()
	_, err := cat.lookup("Atlantis/AtlantisTime")
	if err == nil {
		t.Fatal("expected error for unknown tz, got nil")
	}
	if !strings.Contains(err.Error(), "Atlantis/AtlantisTime") {
		t.Errorf("error should name the bad tz: %v", err)
	}
}

func TestLookupRejectsEmpty(t *testing.T) {
	cat := newTzCatalogue()
	_, err := cat.lookup("")
	if err == nil {
		t.Fatal("expected error for empty tz, got nil")
	}
}

func TestNameRoundtrip(t *testing.T) {
	cat := newTzCatalogue()
	id, err := cat.lookup("Europe/Berlin")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	name, ok := cat.name(id)
	if !ok {
		t.Fatal("name(id) returned !ok for freshly interned id")
	}
	if name != "Europe/Berlin" {
		t.Errorf("name: want %q, got %q", "Europe/Berlin", name)
	}
}

func TestIanaNameSystemResolves(t *testing.T) {
	cat := newTzCatalogue()
	name, err := cat.ianaName(TzIDSystem)
	if err != nil {
		t.Skipf("host zone has no IANA name: %v", err)
	}
	if name == "" || name == "Local" {
		t.Errorf("System should resolve to an IANA zone name, got %q", name)
	}
}

func TestLocationUTC(t *testing.T) {
	cat := newTzCatalogue()
	loc, err := cat.location(TzIDUTC)
	if err != nil {
		t.Fatalf("location UTC: %v", err)
	}
	if loc.String() != "UTC" {
		t.Errorf("UTC location string: want %q, got %q", "UTC", loc.String())
	}
}

func TestLocationUnknownIDFails(t *testing.T) {
	cat := newTzCatalogue()
	_, err := cat.location(60000)
	if err == nil {
		t.Fatal("expected error for unknown TzID, got nil")
	}
}

// With TZ unset Go names the host zone "Local", which ClickHouse rejects;
// System must resolve through /etc/localtime instead.
func TestResolveSystemZone(t *testing.T) {
	noLink := func(string) (string, error) { return "", os.ErrNotExist }
	link := func(target string) func(string) (string, error) {
		return func(string) (string, error) { return target, nil }
	}
	cases := []struct {
		local    string
		readlink func(string) (string, error)
		want     string
		wantErr  bool
	}{
		{"Europe/Berlin", noLink, "Europe/Berlin", false},
		{"UTC", noLink, "UTC", false},
		{"/usr/share/zoneinfo/Asia/Tokyo", noLink, "Asia/Tokyo", false},
		{"Local", link("/usr/share/zoneinfo/Europe/Zurich"), "Europe/Zurich", false},
		{"Local", link("../usr/share/zoneinfo/America/New_York"), "America/New_York", false},
		{"Local", noLink, "", true},
		{"Local", link("/etc/some-copied-file"), "", true},
	}
	for _, tc := range cases {
		got, err := resolveSystemZone(tc.local, tc.readlink)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("resolveSystemZone(%q): got (%q, %v), want %q (err=%v)", tc.local, got, err, tc.want, tc.wantErr)
		}
	}
}
