package example

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stretchr/testify/require"
)

// ipv4ToU32 encodes an IPv4 host address as the big-endian uint32 the leeway
// network canonical uses for ipv4 (v) columns — the Arrow representation
// ClickHouse's IPv4 column round-trips (toIPv4(0x01020304) = '1.2.3.4').
func ipv4ToU32(a netip.Addr) uint32 {
	b := a.As4()
	return binary.BigEndian.Uint32(b[:])
}

// packPrefix4 / packPrefix16 encode a netip.Prefix the way the leeway network
// canonical does: the address bytes followed by one trailing prefix-length byte
// (NetworkTypeAstNode.ByteWidth). This is the write-side mirror of the read
// accessor's GetAttrValue<Col>Prefix decode.
func packPrefix4(p netip.Prefix) (out [5]byte) {
	a := p.Addr().As4()
	copy(out[:4], a[:])
	out[4] = byte(p.Bits())
	return
}

func packPrefix16(p netip.Prefix) (out [17]byte) {
	a := p.Addr().As16()
	copy(out[:16], a[:])
	out[16] = byte(p.Bits())
	return
}

// TestNetworkRoundtrip drives the network table end to end: ipv4/ipv6 host
// addresses and CIDR prefixes are written through the DML FixedSizeBinary
// setters, transferred to an arrow record, loaded on the RA side, and read back
// through every accessor — the packed [N]byte getter and the netip.Addr /
// netip.Prefix convenience accessors — asserting write == read for each.
func TestNetworkRoundtrip(t *testing.T) {
	const nEntities = 3
	addr4 := []netip.Addr{
		netip.MustParseAddr("192.168.0.1"),
		netip.MustParseAddr("10.0.0.42"),
		netip.MustParseAddr("172.16.5.9"),
	}
	addr6 := []netip.Addr{
		netip.MustParseAddr("2001:db8::1"),
		netip.MustParseAddr("2001:db8::dead:beef"),
		netip.MustParseAddr("2001:db8:1:2:3:4:5:6"),
	}
	pfx4 := []netip.Prefix{
		netip.MustParsePrefix("192.168.0.0/24"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/16"),
	}
	pfx6 := []netip.Prefix{
		netip.MustParsePrefix("2001:db8::/48"),
		netip.MustParsePrefix("2001:db8:1::/64"),
		netip.MustParsePrefix("::/0"),
	}
	ts := time.UnixMilli(time.Now().UnixMilli()).UTC()

	dml := NewInEntityNetTable(memory.DefaultAllocator, nEntities)
	var err error
	{ // write via the DML FixedSizeBinary setters
		secNet := dml.GetSectionNet()
		for i := range nEntities {
			ent := dml.BeginEntity()
			ent.SetId(uint64(i))
			ent.SetTimestamp(ts)
			a6 := addr6[i].As16()
			secNet.BeginAttribute(ipv4ToU32(addr4[i]), a6, packPrefix4(pfx4[i]), packPrefix16(pfx6[i])).
				AddMembershipLowCardRef(uint64(i)).
				AddMembershipMixedLowCardVerbatim([]byte("v"), []byte("p")).
				EndAttribute()
			require.NoError(t, ent.CheckErrors())
			require.NoError(t, ent.CommitEntity())
		}
	}

	ra := NewReadAccessNetTable()
	{ // transfer through arrow
		var records []arrow.RecordBatch
		records, err = dml.TransferRecords(nil)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.EqualValues(t, nEntities, records[0].NumRows())
		require.NoError(t, ra.LoadFromRecord(records[0]))
	}

	{ // read back + assert every accessor
		attrs := ra.Net.Attributes
		const attrIdx = runtime.AttributeIdx(0)
		for i := range nEntities {
			entityIdx := runtime.EntityIdx(i)
			require.EqualValues(t, 1, attrs.GetNumberOfAttributes(entityIdx))

			// packed getters: ipv4 is a big-endian uint32, ipv6 a [16]byte
			require.Equal(t, ipv4ToU32(addr4[i]), attrs.GetAttrValueIpv4(entityIdx, attrIdx), "entity %d ipv4 uint32", i)
			require.Equal(t, addr6[i].As16(), attrs.GetAttrValueIpv6(entityIdx, attrIdx), "entity %d ipv6 bytes", i)
			require.Equal(t, packPrefix4(pfx4[i]), attrs.GetAttrValueIpv4Cidr(entityIdx, attrIdx), "entity %d ipv4 cidr bytes", i)
			require.Equal(t, packPrefix16(pfx6[i]), attrs.GetAttrValueIpv6Cidr(entityIdx, attrIdx), "entity %d ipv6 cidr bytes", i)

			// netip.Addr / netip.Prefix convenience accessors
			require.Equal(t, addr4[i], attrs.GetAttrValueIpv4Addr(entityIdx, attrIdx), "entity %d ipv4 addr", i)
			require.Equal(t, addr6[i], attrs.GetAttrValueIpv6Addr(entityIdx, attrIdx), "entity %d ipv6 addr", i)
			require.Equal(t, pfx4[i], attrs.GetAttrValueIpv4CidrPrefix(entityIdx, attrIdx), "entity %d ipv4 prefix", i)
			require.Equal(t, pfx6[i], attrs.GetAttrValueIpv6CidrPrefix(entityIdx, attrIdx), "entity %d ipv6 prefix", i)
		}
	}
}
