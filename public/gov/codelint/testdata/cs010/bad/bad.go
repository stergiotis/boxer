package bad

import "iter"

type Store struct {
	items []int
}

func (inst *Store) Iterate() iter.Seq[int] { // want CS010 here
	return func(yield func(int) bool) {}
}

func (inst *Store) Items() iter.Seq2[int, int] { // want CS010 here
	return func(yield func(int, int) bool) {}
}

func (inst *Store) Stream() iter.Seq[int] { // want CS010 here
	return func(yield func(int) bool) {}
}

func (inst *Store) ScanE() (iter.Seq[int], error) { // want CS010 — iter return alongside error
	return nil, nil
}

func (inst *Store) Suppressed() iter.Seq[int] { //boxer:lint disable=CS010 reason="testdata coverage of suppression"
	return func(yield func(int) bool) {}
}
