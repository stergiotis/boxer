package text

import (
	"encoding/binary"
	fifo "github.com/scalalang2/golang-fifo"
	"github.com/scalalang2/golang-fifo/sieve"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"math"
)

type retrType struct {
	size           imgui.ImVec2
	remainingBytes imgui.Size_t
}

type Cache struct {
	fifo fifo.Cache[string, *retrType]
}

func NewCache(size int) *Cache {
	return &Cache{fifo: sieve.New[string, *retrType](size)}
}

func (inst *Cache) CalcTextSizeA(font imgui.ImFontPtr, size float32, maxWidth float32, wrapWidth float32, text string, pixelPerfect bool) (r imgui.ImVec2, remainingBytes imgui.Size_t, cacheHit bool) {
	var key string
	{
		sizeBits := math.Float32bits(size)
		maxWidthBits := math.Float32bits(maxWidth)
		wrapWidthBits := math.Float32bits(wrapWidth)
		var pixelPerfectBits uint8
		if pixelPerfect {
			pixelPerfectBits = 1
		} else {
			pixelPerfectBits = 0
		}

		b := make([]byte, len(text)+3*4+1, len(text)+3*4+1)
		binary.LittleEndian.PutUint32(b, sizeBits)
		binary.LittleEndian.PutUint32(b[4:], maxWidthBits)
		binary.LittleEndian.PutUint32(b[8:], wrapWidthBits)
		b[8+4+1] = pixelPerfectBits
		copy(b[8+4+1+1:], text)
		key = string(b)
	}

	f := inst.fifo
	var retr *retrType
	retr, cacheHit = f.Get(key)
	if cacheHit {
		r = retr.size
		remainingBytes = retr.remainingBytes
	} else {
		r, remainingBytes = font.CalcTextSizeA(size, maxWidth, wrapWidth, text, pixelPerfect)
		retr = &retrType{
			size:           r,
			remainingBytes: remainingBytes,
		}
		f.Set(key, retr)
	}
	return
}
