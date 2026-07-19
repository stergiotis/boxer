package marshallreflect_test

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// parityNestedWindow is the shared nested attribute struct; parityNested
// binds it at all three nested cardinalities (One / Optional / Many). Each
// binding gets its own section — the shared builder allows one tuple / nested
// field per section ("two tuple fields map one section").
// Parsed AND compiled — see parity_corpus_test.go.
type parityNestedWindow struct {
	Begin time.Time `lw:"beginIncl"`
	End   time.Time `lw:"endExcl"`
}

type parityNested struct {
	_       struct{}                          `kind:"parityNested"`
	ID      uint64                            `lw:",id"`
	Track   []byte                            `lw:",naturalKey"`
	Win     parityNestedWindow                `lw:"win,oneRange"`
	MayWin  option.Option[parityNestedWindow] `lw:"mayWin,optRange"`
	Windows []parityNestedWindow              `lw:"windows,manyRange"`
}
