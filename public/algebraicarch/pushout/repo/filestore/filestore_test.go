// filestore validated by the storage conformance suite, plus
// filestore-specific crash artifacts (torn tail) and a broken-store
// smoke proving the suite bites.
package filestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/storagetest"
)

func TestConformance(tt *testing.T) {
	storagetest.Run(tt, func(location string) (repo.StorageI, error) {
		return Open(location)
	})
}

// A torn trailing line (crash mid-append) is dropped; earlier lines
// survive.
func TestLoadApplied_TornTail(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	st, err := Open(dir)
	if err != nil {
		tt.Fatal(err)
	}
	good := t.PatchHash{1}
	if err := st.AppendApplied(ctx, good); err != nil {
		tt.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "applied.txt"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		tt.Fatal(err)
	}
	if _, err := f.WriteString("0102deadbeef"); err != nil { // torn: short, no newline
		tt.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tt.Fatal(err)
	}
	hs, err := st.LoadApplied(ctx)
	if err != nil {
		tt.Fatalf("torn tail must be tolerated, got: %v", err)
	}
	if len(hs) != 1 || hs[0] != good {
		tt.Fatalf("expected the acked prefix only, got %v", hs)
	}
	// A malformed line in the MIDDLE is corruption, not a torn tail.
	if err := os.WriteFile(filepath.Join(dir, "applied.txt"), []byte("garbage\n"+"01"+string(make([]byte, 0))+"\n"), 0o644); err != nil {
		tt.Fatal(err)
	}
	if _, err := st.LoadApplied(ctx); err == nil {
		tt.Fatal("malformed non-tail line must be an error")
	}
}

// forgetfulStore drops everything on Close — the conformance suite must
// catch it in CheckReopenDurability.
type forgetfulStore struct {
	repo.StorageI
	dir string
}

func TestSuiteBites_ForgetfulStore(tt *testing.T) {
	open := func(location string) (repo.StorageI, error) {
		// "Forgets" by opening a fresh throwaway dir each time while
		// pretending it is the same location.
		inner, err := Open(tt.TempDir())
		if err != nil {
			return nil, err
		}
		return &forgetfulStore{StorageI: inner, dir: location}, nil
	}
	if err := storagetest.CheckReopenDurability(context.Background(), open, tt.TempDir()); err == nil {
		tt.Fatal("conformance suite failed to detect a non-durable store")
	}
}
