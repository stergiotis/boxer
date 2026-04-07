package compression

import (
	"io"
	"iter"

	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// useZstdDictOptimization enables preloading the reference text as a raw zstd
// dictionary (initial history). This avoids re-compressing the reference on
// every joint measurement, yielding a large speedup for zstd encoders.
// See: https://maxhalford.github.io/blog/text-classification-zstd/
const useZstdDictOptimization = true

type CompressorI interface {
	io.Writer
	io.Closer
}
type ResettableVoidI interface {
	Reset(w io.Writer)
}
type ResettableErrI interface {
	Reset(w io.Writer) (err error)
}

// Similarity measures compression-based similarity between a fixed reference
// text and arbitrary candidate texts. It is compressor-agnostic and supports
// an optional zstd raw-dictionary optimization.
type Similarity struct {
	inputText          string
	encoder            CompressorI
	szWriter           *ea.SizeMeasureWriter
	inputCompressedLen uint64
	dictEncoder        *zstd.Encoder
}

func ResetCompressor(compressor CompressorI, w io.Writer) (err error) {
	encR, ok := compressor.(ResettableErrI)
	if ok {
		err = encR.Reset(w)
		if err != nil {
			err = eh.Errorf("unable to reset compressor: %w", err)
			return
		}
		return
	}
	encV, ok := compressor.(ResettableVoidI)
	if ok {
		encV.Reset(w)
		return
	}
	return eh.Errorf("compressor is not resettable")
}

func NewSimilarity(referenceText string, compressor CompressorI) (inst *Similarity, err error) {
	w := &ea.SizeMeasureWriter{
		Size: 0,
	}
	inst = &Similarity{
		inputText: referenceText,
		encoder:   compressor,
		szWriter:  w,
	}
	err = ResetCompressor(compressor, w)
	if err != nil {
		err = eh.Errorf("unable to reset compressor: %w", err)
		return
	}
	var l uint64
	l, err = inst.MeasureCompressedLength(referenceText, "")
	if err != nil {
		err = eh.Errorf("unable to measure input text: %w", err)
		return
	}
	log.Info().Int("uncompressedLength", len(referenceText)).Int("compressedLength", int(l)).Float64("ratio", float64(l)/float64(len(referenceText))).Msg("measured input text")
	inst.inputCompressedLen = l

	if useZstdDictOptimization {
		_, ok := compressor.(*zstd.Encoder)
		if ok {
			var dictEnc *zstd.Encoder
			dictEnc, err = zstd.NewWriter(nil,
				zstd.WithEncoderDictRaw(0, unsafeperf.UnsafeStringToBytes(referenceText)))
			if err != nil {
				err = eh.Errorf("unable to create zstd dict encoder: %w", err)
				return
			}
			inst.dictEncoder = dictEnc
			log.Info().Msg("zstd dict optimization enabled")
		}
	}
	return
}

func (inst *Similarity) InputText() string          { return inst.inputText }
func (inst *Similarity) InputCompressedLen() uint64  { return inst.inputCompressedLen }
func (inst *Similarity) HasDictOptimization() bool   { return inst.dictEncoder != nil }
func (inst *Similarity) Encoder() CompressorI        { return inst.encoder }

func (inst *Similarity) MeasureCompressedLength(text1 string, text2 string) (compressedLen uint64, err error) {
	szWriter := inst.szWriter
	szWriter.Size = 0
	enc := inst.encoder
	err = ResetCompressor(enc, szWriter)
	if err != nil {
		err = eh.Errorf("unable to reset compressor: %w", err)
		return
	}
	if len(text1) > 0 {
		_, err = enc.Write(unsafeperf.UnsafeStringToBytes(text1))
		if err != nil {
			err = eh.Errorf("unable to write to encoder: %w", err)
			return
		}
	}
	if len(text2) > 0 {
		_, err = enc.Write(unsafeperf.UnsafeStringToBytes(text2))
		if err != nil {
			err = eh.Errorf("unable to write to encoder: %w", err)
			return
		}
	}
	err = enc.Close()
	if err != nil {
		err = eh.Errorf("unable to close compressor: %w", err)
		return
	}
	compressedLen = szWriter.Size
	return
}

// MeasureCompressedLengthWithDict compresses only text using the dedicated
// dict encoder (reference text preloaded as initial zstd history). Returns the
// compressed length of text alone. Only valid when HasDictOptimization() is true.
func (inst *Similarity) MeasureCompressedLengthWithDict(text string) (compressedLen uint64, err error) {
	szWriter := inst.szWriter
	szWriter.Size = 0
	inst.dictEncoder.Reset(szWriter)
	if len(text) > 0 {
		_, err = inst.dictEncoder.Write(unsafeperf.UnsafeStringToBytes(text))
		if err != nil {
			err = eh.Errorf("unable to write to dict encoder: %w", err)
			return
		}
	}
	err = inst.dictEncoder.Close()
	if err != nil {
		err = eh.Errorf("unable to close dict encoder: %w", err)
		return
	}
	compressedLen = szWriter.Size
	return
}

// MeasureJointCompressedLength measures C(referenceText || text). Uses the dict
// optimization when available, approximating C(xy) as C(x) + C_dict(y).
func (inst *Similarity) MeasureJointCompressedLength(text string) (compressedLen uint64, err error) {
	if inst.dictEncoder != nil {
		var dictLen uint64
		dictLen, err = inst.MeasureCompressedLengthWithDict(text)
		if err != nil {
			return
		}
		compressedLen = inst.inputCompressedLen + dictLen
		return
	}
	compressedLen, err = inst.MeasureCompressedLength(inst.inputText, text)
	return
}

func (inst *Similarity) MeasureCompressedLengthMany(text1 string, text2Iter iter.Seq[string]) (uncompressedLenText2 uint64, count int64, compressedLen uint64, err error) {
	szWriter := inst.szWriter
	szWriter.Size = 0
	enc := inst.encoder
	err = ResetCompressor(enc, szWriter)
	if err != nil {
		err = eh.Errorf("unable to reset compressor: %w", err)
		return
	}
	if len(text1) > 0 {
		_, err = enc.Write(unsafeperf.UnsafeStringToBytes(text1))
		if err != nil {
			err = eh.Errorf("unable to write to encoder: %w", err)
			return
		}
	}
	for text2 := range text2Iter {
		count++
		var n int
		n, err = enc.Write(unsafeperf.UnsafeStringToBytes(text2))
		uncompressedLenText2 += uint64(n)
		if err != nil {
			err = eh.Errorf("unable to write to encoder: %w", err)
			return
		}
	}
	err = enc.Close()
	if err != nil {
		err = eh.Errorf("unable to close compressor: %w", err)
		return
	}
	compressedLen = szWriter.Size
	return
}

func CalculateNormalizedCompressionDistance(xy, x, y uint64) float64 {
	return (float64(xy) - float64(min(x, y))) / float64(max(x, y))
}

func CalculateConditionalComplexityOfCompression(xy, x uint64) float64 {
	return float64(xy) - float64(x)
}
