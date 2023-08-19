package builder

import (
	"fmt"
	"github.com/rs/zerolog"
	"net"
	"time"
)

type CborKVBuilder[R any] interface {
	Str(key, val string) R
	Strs(key string, vals []string) R
	Stringer(key string, val fmt.Stringer) R
	Stringers(key string, vals []fmt.Stringer) R
	Bytes(key string, val []byte) R
	Hex(key string, val []byte) R
	RawJSON(key string, b []byte) R
	RawCBOR(key string, b []byte) R
	Bool(key string, b bool) R
	Bools(key string, b []bool) R
	Int(key string, i int) R
	Ints(key string, i []int) R
	Int8(key string, i int8) R
	Ints8(key string, i []int8) R
	Int16(key string, i int16) R
	Ints16(key string, i []int16) R
	Int32(key string, i int32) R
	Ints32(key string, i []int32) R
	Int64(key string, i int64) R
	Ints64(key string, i []int64) R
	Uint(key string, i uint) R
	Uints(key string, i []uint) R
	Uint8(key string, i uint8) R
	Uints8(key string, i []uint8) R
	Uint16(key string, i uint16) R
	Uints16(key string, i []uint16) R
	Uint32(key string, i uint32) R
	Uints32(key string, i []uint32) R
	Uint64(key string, i uint64) R
	Uints64(key string, i []uint64) R
	Float32(key string, f float32) R
	Floats32(key string, f []float32) R
	Float64(key string, f float64) R
	Floats64(key string, f []float64) R
	Time(key string, t time.Time) R
	Times(key string, t []time.Time) R
	IPAddr(key string, ip net.IP) R
}

var _ CborKVBuilder[*zerolog.Event] = (*zerolog.Event)(nil)
