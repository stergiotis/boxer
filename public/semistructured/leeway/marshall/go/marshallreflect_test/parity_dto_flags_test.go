package marshallreflect_test

import "time"

// parityFlags covers the simple subset's flag tokens: a `,unit` scalar, a
// `_`-declared `const=`, and the `,ct=` canonical relabels (ADR-0101).
// Parsed AND compiled — see parity_corpus_test.go.
type parityFlags struct {
	_    struct{}  `kind:"parityFlags"`
	_    struct{}  `lw:"env,symbol,const=prod"`
	ID   uint64    `lw:",id"`
	Ts   time.Time `lw:",ts"`
	Host string    `lw:"host,symbol,unit"`
	Src  uint32    `lw:"src,ipv4Section,ct=v"`
	Dst  [16]byte  `lw:"dst,ipv6Section,ct=w"`
}
