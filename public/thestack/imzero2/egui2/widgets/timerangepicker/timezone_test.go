package timerangepicker

import (
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
		t.Fatalf("ianaName System: %v", err)
	}
	if name == "" {
		t.Error("System should resolve to a non-empty zone name (time.Local.String())")
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
