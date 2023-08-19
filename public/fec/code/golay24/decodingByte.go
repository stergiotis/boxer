package golay24

func DecodeLowLevel0(dstPreallocated []byte, g24 []byte, nCodeWords uint32) {
	_ = g24[nCodeWords*3-1] // try bound check elision
	// even codewords
	// fill in byte 0,1, 3,4, 6,7, ...
	for i := uint32(0); i < nCodeWords; i += 2 {
		codeWord := (uint32(g24[i*3+0]) << 16) | (uint32(g24[i*3+1]) << 8) | uint32(g24[i*3+2])
		decoded := DecodeSingle(codeWord)
		p := 3 * i / 2
		dstPreallocated[p+0] = byte(decoded >> 4)
		dstPreallocated[p+1] = byte((decoded & 0b1111) << 4)
	}
	// odd codewords
	// fill in byte 1,2, 4,5, 7,8, ...
	for i := uint32(1); i < nCodeWords; i += 2 {
		codeWord := (uint32(g24[i*3+0]) << 16) | (uint32(g24[i*3+1]) << 8) | uint32(g24[i*3+2])
		decoded := DecodeSingle(codeWord)
		p := 3 * (i - 1) / 2
		dstPreallocated[p+1] |= byte(decoded >> 8)
		dstPreallocated[p+2] = byte(decoded & 0xff)
	}
}

func DecodeLowLevel1(dstPreallocated []byte, g24 []byte, nCodeWords uint32) {
	codeWord := uint32(0)
	p := 0
	for i, b := range g24 {
		m := i % 3
		s := 2 - m
		codeWord |= uint32(b) << (s * 8)
		if s == 0 {
			decoded := DecodeSingle(codeWord)
			codeWord = 0

			if (i+1)%6 == 0 {
				// odd code word
				dstPreallocated[p+1] |= byte(decoded >> 8)
				dstPreallocated[p+2] = byte(decoded & 0xff)
				p += 3
			} else {
				// even code word
				dstPreallocated[p+0] = byte(decoded >> 4)
				dstPreallocated[p+1] = byte((decoded & 0b1111) << 4)
			}
		}
	}
}

func DecodeLowLevel2(dstPreallocated []byte, g24 []byte, nCodeWords uint32) {
	codeWord := uint32(0)
	next := 0
	even := true
	carry := byte(0)
	p := 0
	for _, b := range g24 {
		switch next {
		case 0:
			codeWord = uint32(b) << 16
			next = 1
			break
		case 1:
			codeWord |= uint32(b) << 8
			next = 2
			break
		case 2:
			codeWord |= uint32(b)
			decoded := DecodeSingle(codeWord)
			next = 0

			if even {
				// even code word
				dstPreallocated[p] = byte(decoded >> 4)
				p++
				carry = byte((decoded & 0b1111) << 4)
				even = false
			} else {
				// odd code word
				dstPreallocated[p] = carry | byte(decoded>>8)
				p++
				dstPreallocated[p] = byte(decoded)
				p++
				even = true
			}
			break
		}
	}
}

func DecodeLowLevel(dstPreallocated []byte, g24 []byte) {
	codeWord := uint32(0)
	next := 0
	even := true
	carry := byte(0)
	p := 0
	for _, b := range g24 {
		switch next {
		case 0:
			codeWord = uint32(b) << 16
			next = 1
			break
		case 1:
			codeWord |= uint32(b) << 8
			next = 2
			break
		case 2:
			codeWord |= uint32(b)
			decoded := DecodeSingle(codeWord)
			next = 0

			if even {
				// even code word
				dstPreallocated[p] = byte(decoded >> 4)
				p++
				carry = byte((decoded & 0b1111) << 4)
				even = false
			} else {
				// odd code word
				dstPreallocated[p] = carry | byte(decoded>>8)
				p++
				dstPreallocated[p] = byte(decoded)
				p++
				even = true
			}
			break
		}
	}

	return
}
