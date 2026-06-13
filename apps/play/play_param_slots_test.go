package play

import (
	"testing"
)

func TestExtractParamSlotsSingle(t *testing.T) {
	slots, err := ExtractParamSlots(`SELECT {a : UInt64}`)
	if err != nil {
		t.Fatalf("ExtractParamSlots: %v", err)
	}
	if got, want := len(slots), 1; got != want {
		t.Fatalf("len(slots) = %d, want %d", got, want)
	}
	if got, want := slots[0].Name, "a"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := slots[0].Type, "UInt64"; got != want {
		t.Errorf("Type = %q, want %q", got, want)
	}
}

func TestExtractParamSlotsPair(t *testing.T) {
	slots, err := ExtractParamSlots(
		`SELECT * FROM t WHERE ts BETWEEN {from : DateTime} AND {to : DateTime}`)
	if err != nil {
		t.Fatalf("ExtractParamSlots: %v", err)
	}
	if got, want := len(slots), 2; got != want {
		t.Fatalf("len(slots) = %d, want %d", got, want)
	}
	if slots[0].Name != "from" || slots[1].Name != "to" {
		t.Errorf("names = %q,%q want from,to", slots[0].Name, slots[1].Name)
	}
	if slots[0].Type != "DateTime" || slots[1].Type != "DateTime" {
		t.Errorf("types = %q,%q want DateTime", slots[0].Type, slots[1].Type)
	}
}

func TestExtractParamSlotsDedupes(t *testing.T) {
	slots, err := ExtractParamSlots(
		`SELECT {a : UInt64} + {a : UInt64} AS twice FROM t WHERE x = {a : UInt64}`)
	if err != nil {
		t.Fatalf("ExtractParamSlots: %v", err)
	}
	if got, want := len(slots), 1; got != want {
		t.Fatalf("len(slots) = %d, want %d (dedup expected)", got, want)
	}
	if slots[0].Name != "a" {
		t.Errorf("Name = %q, want a", slots[0].Name)
	}
}

func TestExtractParamSlotsNone(t *testing.T) {
	slots, err := ExtractParamSlots(`SELECT 1`)
	if err != nil {
		t.Fatalf("ExtractParamSlots: %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("len(slots) = %d, want 0", len(slots))
	}
}

func TestExtractParamSlotsComplexType(t *testing.T) {
	slots, err := ExtractParamSlots(`SELECT {x : Nullable(String)}`)
	if err != nil {
		t.Fatalf("ExtractParamSlots: %v", err)
	}
	if got, want := len(slots), 1; got != want {
		t.Fatalf("len(slots) = %d, want %d", got, want)
	}
	if got, want := slots[0].Type, "Nullable(String)"; got != want {
		t.Errorf("Type = %q, want %q", got, want)
	}
}

func TestExtractParamSlotsSyntaxError(t *testing.T) {
	_, err := ExtractParamSlots(`THIS IS NOT SQL`)
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
}
