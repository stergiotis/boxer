package csr

// radixSortUint64 sorts a in place by LSD radix on 11-bit digits, using
// scratch (grown as needed) as the ping-pong buffer. A digit that every key
// shares is skipped, so keys drawn from a small range cost few passes.
func radixSortUint64(a []uint64, scratch []uint64) []uint64 {
	const bits = 11
	const buckets = 1 << bits
	const mask = buckets - 1
	if len(a) < 2 {
		return scratch
	}
	if cap(scratch) < len(a) {
		scratch = make([]uint64, len(a))
	}
	scratch = scratch[:len(a)]
	var orAll, andAll uint64
	andAll = ^uint64(0)
	for _, v := range a {
		orAll |= v
		andAll &= v
	}
	varying := orAll &^ andAll // bits that differ between some pair of keys
	src, dst := a, scratch
	var count [buckets]int
	for shift := 0; shift < 64; shift += bits {
		if (varying>>shift)&mask == 0 {
			continue
		}
		clear(count[:])
		for _, v := range src {
			count[(v>>shift)&mask]++
		}
		sum := 0
		for i := range count {
			c := count[i]
			count[i] = sum
			sum += c
		}
		for _, v := range src {
			d := (v >> shift) & mask
			dst[count[d]] = v
			count[d]++
		}
		src, dst = dst, src
	}
	if &src[0] != &a[0] {
		copy(a, src)
	}
	return scratch
}
