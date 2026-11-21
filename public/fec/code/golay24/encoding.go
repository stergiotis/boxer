package golay24

// encodingTriples is encoding laid out as three big-endian bytes per word.
var encodingTriples = encodeTableToUint8Triples(encoding)

// EncodeWord returns the 24-bit codeword of the low 12 bits of w.
func EncodeWord(w uint16) (g uint32) {
	return encoding[w&0xfff]
}

// PutEncoded writes the codeword of the low 12 bits of w to dst[0:3],
// big-endian. dst must hold three bytes.
func PutEncoded(dst []byte, w uint16) {
	t := 3 * int(w&0xfff)
	_ = dst[2]
	dst[0] = encodingTriples[t]
	dst[1] = encodingTriples[t+1]
	dst[2] = encodingTriples[t+2]
}

func encodeTableToUint8Triples(enc [4096]uint32) [12288]uint8 {
	r := [12288]uint8{}
	for i := range len(enc) {
		r[3*i+0] = uint8(enc[i] >> 16)
		r[3*i+1] = uint8(enc[i] >> 8)
		r[3*i+2] = uint8(enc[i])
	}
	return r
}

func EncodeBytes(dstP []byte, p []byte) (dst []byte, err error) {
	dst = dstP
	c := len(p)*3/2 + 1
	if dst == nil {
		dst = make([]byte, 0, c)
	}
	l := len(p)
	w1 := uint16(0)
	w2 := uint16(0)
	g1 := uint32(0)
	g2 := uint32(0)
	i := 0
	for ; i < l; i += 3 {
		w1 = uint16(p[i]) << 4
		w1 |= uint16(p[i+1] >> 4)
		w2 = uint16(p[i+1]&0xf) << 8
		w2 |= uint16(p[i+2])
		// map 3/2 bytes = 12 bits = 3 nibbles to
		//       3 bytes = 24 bits = 6 nibbles
		g1 = encoding[w1]
		g2 = encoding[w2]
		dst = append(dst,
			uint8(g1>>16),
			uint8(g1>>8),
			uint8(g1),
			uint8(g2>>16),
			uint8(g2>>8),
			uint8(g2),
		)
	}
	rest := l - i
	switch rest {
	case 0:
		break
	case 1:
		w1 = uint16(p[l-1]) << 4
		g1 = encoding[w1]
		dst = append(dst,
			uint8(g1>>16),
			uint8(g1>>8),
			uint8(g1))
	case 2:
		w1 = uint16(p[l-2]) << 4
		w1 |= uint16(p[l-2] >> 4)
		w2 = uint16(p[l-1]&0xf) << 8
		g1 = encoding[w1]
		g2 = encoding[w2]
		dst = append(dst,
			uint8(g1>>16),
			uint8(g1>>8),
			uint8(g1),
			uint8(g2>>16),
			uint8(g2>>8),
			uint8(g2))
	}
	return
}
