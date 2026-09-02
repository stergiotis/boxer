package runtime

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// This file carries the ADR-0210 SD3 value forms over the Go types the
// generated readaccess accessors hand out and the generated dml builders
// take: sized integers, float32/float64, string, []byte, bool, time.Time and
// the network lanes (netip.Addr / netip.Prefix, or their packed uint32 /
// [16]byte / [5]byte / [17]byte forms).
//
// The forms are ADR-0201 SD3's, minus what that form erases. There is no
// numeric reduction — a float stays a float, −0.0 keeps its sign — and no
// IPv4-mapped reduction: an IPv6 value in ::ffff:0:0/96 travels as IPv6.
// Fixed-width padding is kept, as it is there. The integer, text, byte-string
// and bool forms need nothing beyond CborWriter's and CborReader's own
// methods and are not repeated here.

// failValue records err as the writer's sticky error, unless one is already
// recorded.
func (c *CborWriter) failValue(err error) {
	if c.err == nil {
		c.err = err
	}
}

// WriteF32 writes a float32 in the shortest width that preserves it. A
// float32 widens to float64 exactly, so f32 x and f64(x) are byte-identical.
func (c *CborWriter) WriteF32(f float32) { c.WriteFloatShortest(float64(f)) }

// WriteF64 writes a float64 in the shortest width that preserves it.
func (c *CborWriter) WriteF64(f float64) { c.WriteFloatShortest(f) }

// ReadF32 reads a float and narrows it to float32.
func (c *CborReader) ReadF32() float32 { return c.ReadFloat32() }

// ReadF64 reads a float.
func (c *CborReader) ReadF64() float64 { return c.ReadFloat64() }

// WriteTemporal writes an instant as RFC 9581 tag 1001: a map with key 1 =
// integer Unix seconds (floor) and key −9 = integer nanoseconds, the latter
// present only when non-zero. The keys sort 0x01 < 0x28 bytewise, so the map
// is written 1 then −9. Location is erased: an instant is a point in time,
// and both z32 and z64 lanes encode the same whole second identically. Sub-
// second precision below the nanosecond does not exist on either lane.
func (c *CborWriter) WriteTemporal(t time.Time) {
	// Go normalizes a Time so that Nanosecond is in [0, 1e9) and Unix is the
	// floor second, which is the pre-epoch behaviour the form asks for.
	c.WriteTemporalParts(t.Unix(), int64(t.Nanosecond()))
}

// WriteTemporalParts writes the tag 1001 item from an already-split instant:
// floor seconds and nanoseconds in [0, 1e9). It is the one implementation of
// the RFC 9581 emission — canonform derives the parts from raw lanes and
// writes through here, so the wire and the quotient cannot drift on this
// rule.
func (c *CborWriter) WriteTemporalParts(secs int64, nanos int64) {
	if nanos < 0 || nanos > 999_999_999 {
		c.failValue(eb.Build().Int64("nanoseconds", nanos).Errorf("nanoseconds must be in [0, 999999999]: %w", ErrOutOfRange))
		return
	}
	c.Tag(TagExtendedTime)
	if nanos == 0 {
		c.MapHead(1)
		c.WriteUint(1)
		c.WriteInt(secs)
		return
	}
	c.MapHead(2)
	c.WriteUint(1)
	c.WriteInt(secs)
	c.WriteInt(-9)
	c.WriteUint(uint64(nanos))
}

// ReadTemporal reads an RFC 9581 tag 1001 instant and returns it in UTC. The
// map must be in the canonical shape WriteTemporal produces: key 1 first, key
// −9 only when the nanoseconds are non-zero.
func (c *CborReader) ReadTemporal() (t time.Time) {
	c.ExpectTag(TagExtendedTime)
	n := c.ReadMapHead()
	if c.err != nil {
		return
	}
	if n != 1 && n != 2 {
		c.fail(eb.Build().Int("pos", c.pos).Int("entries", n).Errorf("temporal map must hold one or two entries: %w", ErrOutOfRange))
		return
	}
	if k := c.ReadInt(); c.err == nil && k != 1 {
		c.fail(eb.Build().Int("pos", c.pos).Int64("key", k).Errorf("temporal map must start with key 1: %w", ErrOutOfRange))
		return
	}
	secs := c.ReadInt()
	var nanos int64
	if n == 2 {
		if k := c.ReadInt(); c.err == nil && k != -9 {
			c.fail(eb.Build().Int("pos", c.pos).Int64("key", k).Errorf("the second temporal map key must be -9: %w", ErrOutOfRange))
			return
		}
		v := c.ReadUint()
		if c.err != nil {
			return
		}
		if v == 0 || v > 999_999_999 {
			c.fail(eb.Build().Int("pos", c.pos).Uint64("nanoseconds", v).Errorf("nanoseconds must be in [1, 999999999]; zero is omitted: %w", ErrOutOfRange))
			return
		}
		nanos = int64(v)
	}
	if c.err != nil {
		return
	}
	return time.Unix(secs, nanos).UTC()
}

// WritePrefixContent writes the RFC 9164 §3.2 prefix content:
// [prefix-length, address bytes with the bits beyond the prefix zeroed and
// trailing zero bytes omitted]. Host bits are not content — a prefix travels
// masked, so an unmasked netip.Prefix does not survive the round trip
// unchanged. It is the one implementation of the masking rule — canonform
// applies its IPv4-mapped reduction first and writes through here, so the
// wire and the quotient cannot drift on it. The caller writes the tag.
func (c *CborWriter) WritePrefixContent(addr []byte, bits int) {
	if bits < 0 || bits > len(addr)*8 {
		c.failValue(eb.Build().Int("bits", bits).Int("addrLen", len(addr)).Errorf("prefix length exceeds the address width: %w", ErrOutOfRange))
		return
	}
	var buf [16]byte
	n := copy(buf[:], addr)
	full := bits / 8
	if rem := bits % 8; rem != 0 {
		buf[full] &= byte(0xff << (8 - rem))
		full++
	}
	for k := full; k < n; k++ {
		buf[k] = 0
	}
	end := full
	for end > 0 && buf[end-1] == 0 {
		end--
	}
	c.ArrayHead(2)
	c.WriteUint(uint64(bits))
	c.WriteBytes(buf[:end])
}

// WriteIPv4 writes a four-byte address under RFC 9164 tag 52.
func (c *CborWriter) WriteIPv4(a netip.Addr) {
	if !a.Is4() {
		c.failValue(eb.Build().Str("addr", a.String()).Errorf("address is not IPv4: %w", ErrOutOfRange))
		return
	}
	b := a.As4()
	c.Tag(TagIPv4)
	c.WriteBytes(b[:])
}

// WriteIPv6 writes a sixteen-byte address under RFC 9164 tag 54. An
// IPv4-mapped address stays IPv6: ADR-0210 SD3 drops the reduction ADR-0201
// applies, so a v → w widening is visible in the bytes and losslessly
// reversible.
func (c *CborWriter) WriteIPv6(a netip.Addr) {
	if !a.Is6() {
		c.failValue(eb.Build().Str("addr", a.String()).Errorf("address is not IPv6: %w", ErrOutOfRange))
		return
	}
	if a.Zone() != "" {
		c.failValue(eb.Build().Str("addr", a.String()).Errorf("zoned addresses are not part of the form: %w", ErrOutOfRange))
		return
	}
	b := a.As16()
	c.Tag(TagIPv6)
	c.WriteBytes(b[:])
}

// WriteAddr writes an address under the tag its family selects: tag 52 for a
// four-byte address, tag 54 for a sixteen-byte one, IPv4-mapped included.
func (c *CborWriter) WriteAddr(a netip.Addr) {
	if a.Is4() {
		c.WriteIPv4(a)
		return
	}
	c.WriteIPv6(a)
}

// WriteIPv4Prefix writes an IPv4 prefix under RFC 9164 tag 52.
func (c *CborWriter) WriteIPv4Prefix(p netip.Prefix) {
	a := p.Addr()
	if !a.Is4() {
		c.failValue(eb.Build().Str("prefix", p.String()).Errorf("prefix is not IPv4: %w", ErrOutOfRange))
		return
	}
	b := a.As4()
	c.Tag(TagIPv4)
	c.WritePrefixContent(b[:], p.Bits())
}

// WriteIPv6Prefix writes an IPv6 prefix under RFC 9164 tag 54, with no
// IPv4-mapped reduction.
func (c *CborWriter) WriteIPv6Prefix(p netip.Prefix) {
	a := p.Addr()
	if !a.Is6() {
		c.failValue(eb.Build().Str("prefix", p.String()).Errorf("prefix is not IPv6: %w", ErrOutOfRange))
		return
	}
	if a.Zone() != "" {
		c.failValue(eb.Build().Str("prefix", p.String()).Errorf("zoned addresses are not part of the form: %w", ErrOutOfRange))
		return
	}
	b := a.As16()
	c.Tag(TagIPv6)
	c.WritePrefixContent(b[:], p.Bits())
}

// WritePrefix writes a prefix under the tag its family selects.
func (c *CborWriter) WritePrefix(p netip.Prefix) {
	if p.Addr().Is4() {
		c.WriteIPv4Prefix(p)
		return
	}
	c.WriteIPv6Prefix(p)
}

// The Raw variants take the packed lane values the generated readaccess
// getters return and the generated dml builders accept, without a detour
// through net/netip: a bare IPv4 is a big-endian uint32, a bare IPv6 is
// sixteen bytes, and a CIDR value is its address bytes followed by one
// trailing prefix-length byte (the layout NetworkTypeAstNode.ByteWidth
// documents).

// WriteIPv4Raw writes a bare IPv4 lane value — a big-endian uint32.
func (c *CborWriter) WriteIPv4Raw(v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	c.Tag(TagIPv4)
	c.WriteBytes(b[:])
}

// WriteIPv6Raw writes a bare IPv6 lane value — sixteen packed bytes.
func (c *CborWriter) WriteIPv6Raw(v [16]byte) {
	c.Tag(TagIPv6)
	c.WriteBytes(v[:])
}

// WriteIPv4PrefixRaw writes an IPv4 CIDR lane value — four address bytes and
// one prefix-length byte.
func (c *CborWriter) WriteIPv4PrefixRaw(v [5]byte) {
	c.Tag(TagIPv4)
	c.WritePrefixContent(v[:4], int(v[4]))
}

// WriteIPv6PrefixRaw writes an IPv6 CIDR lane value — sixteen address bytes
// and one prefix-length byte.
func (c *CborWriter) WriteIPv6PrefixRaw(v [17]byte) {
	c.Tag(TagIPv6)
	c.WritePrefixContent(v[:16], int(v[16]))
}

// readAddrBytes reads a tagged address's byte string and checks its width.
func (c *CborReader) readAddrBytes(want int) (b []byte) {
	b = c.ReadBytes()
	if c.err != nil {
		return nil
	}
	if len(b) != want {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Int("want", want).Errorf("address is not the width its tag declares: %w", ErrOutOfRange))
		return nil
	}
	return
}

// readPrefixContent reads the RFC 9164 §3.2 prefix content into a zero-padded
// address of addrLen bytes and its prefix length, refusing an encoding a
// canonical writer would not have produced: a prefix longer than the address,
// more bytes than the prefix needs, a trailing zero byte that should have
// been omitted, or a set bit beyond the prefix.
func (c *CborReader) readPrefixContent(addrLen int, addr []byte) (bits int) {
	if n := c.ReadArrayHead(); c.err == nil && n != 2 {
		c.fail(eb.Build().Int("pos", c.pos).Int("elements", n).Errorf("a prefix is a two-element array: %w", ErrOutOfRange))
		return
	}
	v := c.ReadUint()
	if c.err != nil {
		return
	}
	if v > uint64(addrLen*8) {
		c.fail(eb.Build().Int("pos", c.pos).Uint64("bits", v).Int("addrLen", addrLen).Errorf("prefix length exceeds the address width: %w", ErrOutOfRange))
		return
	}
	bits = int(v)
	b := c.ReadBytes()
	if c.err != nil {
		return 0
	}
	need := (bits + 7) / 8
	if len(b) > need {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Int("need", need).Errorf("prefix carries more bytes than its length needs: %w", ErrNonShortest))
		return 0
	}
	if len(b) > 0 && b[len(b)-1] == 0 {
		c.fail(eb.Build().Int("pos", c.pos).Int("len", len(b)).Errorf("trailing zero bytes must be omitted from a prefix: %w", ErrNonShortest))
		return 0
	}
	if rem := bits % 8; rem != 0 && len(b) == need && b[need-1]&byte(0xff>>rem) != 0 {
		c.fail(eb.Build().Int("pos", c.pos).Int("bits", bits).Errorf("bits beyond the prefix length must be zero: %w", ErrNonShortest))
		return 0
	}
	copy(addr, b)
	return
}

// ReadAddr reads a tagged address and returns it; the tag decides the family.
// An IPv4-mapped IPv6 address comes back as IPv6, as it went out.
func (c *CborReader) ReadAddr() (a netip.Addr) {
	tag := c.ReadTag()
	if c.err != nil {
		return
	}
	switch tag {
	case TagIPv4:
		b := c.readAddrBytes(4)
		if c.err != nil {
			return
		}
		return netip.AddrFrom4([4]byte(b))
	case TagIPv6:
		b := c.readAddrBytes(16)
		if c.err != nil {
			return
		}
		return netip.AddrFrom16([16]byte(b))
	}
	c.fail(eb.Build().Int("pos", c.pos).Uint64("tag", tag).Errorf("not a network address tag: %w", ErrOutOfRange))
	return
}

// ReadIPv4 reads an address and requires it to be IPv4 (tag 52).
func (c *CborReader) ReadIPv4() (a netip.Addr) {
	c.ExpectTag(TagIPv4)
	b := c.readAddrBytes(4)
	if c.err != nil {
		return
	}
	return netip.AddrFrom4([4]byte(b))
}

// ReadIPv6 reads an address and requires it to be IPv6 (tag 54).
func (c *CborReader) ReadIPv6() (a netip.Addr) {
	c.ExpectTag(TagIPv6)
	b := c.readAddrBytes(16)
	if c.err != nil {
		return
	}
	return netip.AddrFrom16([16]byte(b))
}

// ReadPrefix reads a tagged prefix and returns it masked; the tag decides the
// family.
func (c *CborReader) ReadPrefix() (p netip.Prefix) {
	tag := c.ReadTag()
	if c.err != nil {
		return
	}
	switch tag {
	case TagIPv4:
		return c.readIPv4PrefixContent()
	case TagIPv6:
		return c.readIPv6PrefixContent()
	}
	c.fail(eb.Build().Int("pos", c.pos).Uint64("tag", tag).Errorf("not a network address tag: %w", ErrOutOfRange))
	return
}

func (c *CborReader) readIPv4PrefixContent() (p netip.Prefix) {
	var addr [4]byte
	bits := c.readPrefixContent(4, addr[:])
	if c.err != nil {
		return
	}
	return netip.PrefixFrom(netip.AddrFrom4(addr), bits)
}

func (c *CborReader) readIPv6PrefixContent() (p netip.Prefix) {
	var addr [16]byte
	bits := c.readPrefixContent(16, addr[:])
	if c.err != nil {
		return
	}
	return netip.PrefixFrom(netip.AddrFrom16(addr), bits)
}

// ReadIPv4Prefix reads a prefix and requires it to be IPv4 (tag 52).
func (c *CborReader) ReadIPv4Prefix() (p netip.Prefix) {
	c.ExpectTag(TagIPv4)
	if c.err != nil {
		return
	}
	return c.readIPv4PrefixContent()
}

// ReadIPv6Prefix reads a prefix and requires it to be IPv6 (tag 54).
func (c *CborReader) ReadIPv6Prefix() (p netip.Prefix) {
	c.ExpectTag(TagIPv6)
	if c.err != nil {
		return
	}
	return c.readIPv6PrefixContent()
}

// ReadIPv4Raw reads a bare IPv4 into the lane's big-endian uint32.
func (c *CborReader) ReadIPv4Raw() (v uint32) {
	c.ExpectTag(TagIPv4)
	b := c.readAddrBytes(4)
	if c.err != nil {
		return
	}
	return binary.BigEndian.Uint32(b)
}

// ReadIPv6Raw reads a bare IPv6 into the lane's sixteen packed bytes.
func (c *CborReader) ReadIPv6Raw() (v [16]byte) {
	c.ExpectTag(TagIPv6)
	b := c.readAddrBytes(16)
	if c.err != nil {
		return
	}
	return [16]byte(b)
}

// ReadIPv4PrefixRaw reads an IPv4 prefix into the lane's four address bytes
// plus one prefix-length byte. The address comes back masked.
func (c *CborReader) ReadIPv4PrefixRaw() (v [5]byte) {
	c.ExpectTag(TagIPv4)
	if c.err != nil {
		return
	}
	bits := c.readPrefixContent(4, v[:4])
	if c.err != nil {
		return [5]byte{}
	}
	v[4] = byte(bits)
	return
}

// ReadIPv6PrefixRaw reads an IPv6 prefix into the lane's sixteen address
// bytes plus one prefix-length byte. The address comes back masked.
func (c *CborReader) ReadIPv6PrefixRaw() (v [17]byte) {
	c.ExpectTag(TagIPv6)
	if c.err != nil {
		return
	}
	bits := c.readPrefixContent(16, v[:16])
	if c.err != nil {
		return [17]byte{}
	}
	v[16] = byte(bits)
	return
}

// ReadSetHead reads the head of a set — tag 258 followed by a definite-length
// array — and returns the element count. It does not check that the elements
// are sorted, because that costs a second pass over bytes the caller is about
// to read anyway: a caller that must enforce it reads the elements with
// ReadItemBytes and compares consecutive views with bytes.Compare, which must
// be non-decreasing.
func (c *CborReader) ReadSetHead() int {
	c.ExpectTag(TagSet)
	if c.err != nil {
		return 0
	}
	return c.ReadArrayHead()
}

// SetWriter builds the ADR-0210 SD3 set form: tag 258 over a definite-length
// array of elements sorted bytewise on their canonical encodings, duplicates
// kept. Set order is not content and sorting is what makes the bytes
// canonical; a set's *length* is content, because an `m` column is a DML
// co-container alongside the section's `h` columns — one element is appended
// to all of them at once — so dropping a duplicate would leave the decoder
// unable to rebuild the attribute.
//
// Elements are written one at a time through a scratch CborWriter into one
// growing buffer; only the ranges are sorted, so an element's bytes are
// written once and copied once. A homogenous array (the h modifier) keeps its
// stored order and needs none of this — ArrayHead(n) followed by the elements.
//
// A SetWriter must not be copied once constructed: its scratch writer points
// at its buffer.
type SetWriter struct {
	cw   *CborWriter
	buf  bytes.Buffer
	ends []int
	ord  []int
	open bool
}

// NewSetWriter returns an empty set writer.
func NewSetWriter() (inst *SetWriter, err error) {
	inst = &SetWriter{}
	inst.cw, err = NewCborWriter(&inst.buf)
	if err != nil {
		inst = nil
		return
	}
	return
}

// Begin drops any elements collected so far and starts a new set. A set
// writer is reused across sets and across entities.
func (s *SetWriter) Begin() {
	s.buf.Reset()
	s.ends = s.ends[:0]
	s.open = false
	s.cw.Reset(&s.buf)
}

// Elem returns the scratch writer positioned for the next element. Everything
// written to it up to the matching EndElem forms one element, which must be
// exactly one complete data item.
func (s *SetWriter) Elem() *CborWriter {
	if s.open {
		s.cw.failValue(eb.Build().Errorf("Elem called twice without EndElem: %w", ErrOutOfRange))
	}
	s.open = true
	return s.cw
}

// EndElem closes the element Elem opened.
func (s *SetWriter) EndElem() {
	if !s.open {
		s.cw.failValue(eb.Build().Errorf("EndElem called without Elem: %w", ErrOutOfRange))
		return
	}
	s.open = false
	s.ends = append(s.ends, s.buf.Len())
}

// Len returns how many elements have been collected.
func (s *SetWriter) Len() int { return len(s.ends) }

// Err returns the first error the scratch writer recorded.
func (s *SetWriter) Err() error { return s.cw.Err() }

// bounds returns the byte range of collected element i.
func (s *SetWriter) bounds(i int) (begin int, end int) {
	if i > 0 {
		begin = s.ends[i-1]
	}
	return begin, s.ends[i]
}

// Flush writes the collected elements to cw as tag 258 over an array, sorted
// bytewise with duplicates kept. The set writer is left empty, ready for the
// next set.
func (s *SetWriter) Flush(cw *CborWriter) {
	defer s.Begin()
	if s.open {
		cw.failValue(eb.Build().Errorf("Flush called with an element still open: %w", ErrOutOfRange))
		return
	}
	if err := s.cw.Err(); err != nil {
		cw.failValue(err)
		return
	}
	n := len(s.ends)
	s.ord = s.ord[:0]
	for i := range n {
		s.ord = append(s.ord, i)
	}
	all := s.buf.Bytes()
	elem := func(i int) []byte {
		b, e := s.bounds(i)
		return all[b:e]
	}
	slices.SortStableFunc(s.ord, func(a int, b int) int {
		return bytes.Compare(elem(a), elem(b))
	})
	cw.Tag(TagSet)
	cw.ArrayHead(len(s.ord))
	for _, o := range s.ord {
		cw.Write(elem(o))
	}
}
