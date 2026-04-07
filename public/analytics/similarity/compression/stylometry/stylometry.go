package stylometry

import (
	"bytes"
	"iter"
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/similarity/compression"
	"github.com/stergiotis/boxer/public/analytics/stats"
)

// Analyzer combines a compression.Similarity engine with convergence detection
// to perform streaming authorship analysis via NCD/CCC metrics.
type Analyzer struct {
	*compression.Similarity
	convergenceDetector *stats.ConvergenceDetector
	buf                 *bytes.Buffer
}

func NewAnalyzer(referenceText string, convergenceDetector *stats.ConvergenceDetector, compressor compression.CompressorI) (inst *Analyzer, err error) {
	var sim *compression.Similarity
	sim, err = compression.NewSimilarity(referenceText, compressor)
	if err != nil {
		return
	}
	inst = &Analyzer{
		Similarity:          sim,
		convergenceDetector: convergenceDetector,
		buf:                 bytes.NewBuffer(make([]byte, 0, len(referenceText)*2)),
	}
	return
}

func (inst *Analyzer) MeasureNcdInstance(texts iter.Seq[string]) (totalLength uint64, count int64, minNcd float64, meanNcd float64, maxNcd float64, stddevNcd float64, converged bool, err error) {
	x := inst.InputCompressedLen()
	c := inst.convergenceDetector
	c.Reset()
	for t2 := range texts {
		var xy, y uint64
		xy, err = inst.MeasureJointCompressedLength(t2)
		if err != nil {
			log.Warn().Err(err).Str("text", t2).Msg("unable to measure joint compressed length, skipping")
			err = nil
			continue
		}
		y, err = inst.MeasureCompressedLength(t2, "")
		if err != nil {
			log.Warn().Err(err).Str("text", t2).Msg("unable to measure compressed length, skipping")
			err = nil
			continue
		}
		ncd := compression.CalculateNormalizedCompressionDistance(xy, x, y)
		if !math.IsNaN(ncd) && !math.IsInf(ncd, 1) && !math.IsInf(ncd, -1) {
			converged = c.Push(ncd)
			totalLength += uint64(len(t2))
			if converged {
				break
			}
		}
	}
	count = c.Count()
	minNcd = c.Min()
	meanNcd = c.Mean()
	maxNcd = c.Max()
	stddevNcd = c.StdDev()
	return
}

func (inst *Analyzer) MeasureCccInstance(texts iter.Seq[string]) (totalLength uint64, count int64, minCcc float64, meanCcc float64, maxCcc float64, stddevCcc float64, converged bool, err error) {
	x := inst.InputCompressedLen()
	c := inst.convergenceDetector
	c.Reset()
	for t2 := range texts {
		var xy uint64
		xy, err = inst.MeasureJointCompressedLength(t2)
		if err != nil {
			log.Warn().Err(err).Str("text", t2).Msg("unable to measure joint compressed length, skipping")
			err = nil
			continue
		}
		ccc := compression.CalculateConditionalComplexityOfCompression(xy, x)
		if !math.IsNaN(ccc) && !math.IsInf(ccc, 1) && !math.IsInf(ccc, -1) {
			converged = c.Push(ccc)
			totalLength += uint64(len(t2))
			if converged {
				break
			}
		}
	}
	count = c.Count()
	minCcc = c.Min()
	meanCcc = c.Mean()
	maxCcc = c.Max()
	stddevCcc = c.StdDev()
	return
}

func (inst *Analyzer) gatherEqualLengthProfile(texts iter.Seq[string]) (t1Trunc string, t2Trunc string, count int64, totalLength uint64) {
	t1 := inst.InputText()
	buf := inst.buf
	buf.Reset()
	l := len(t1)
	for t := range texts {
		count++
		_, _ = buf.WriteString(t)
		if buf.Len() > l {
			break
		}
	}
	totalLength = uint64(min(l, buf.Len()))
	t1Trunc = t1[:totalLength]
	t2Trunc = buf.String()[:totalLength]
	return
}

func (inst *Analyzer) MeasureCccProfile(texts iter.Seq[string]) (totalLength uint64, count int64, ccc float64, err error) {
	var t1Trunc, t2Trunc string
	t1Trunc, t2Trunc, count, totalLength = inst.gatherEqualLengthProfile(texts)
	x := inst.InputCompressedLen()
	if totalLength != uint64(len(inst.InputText())) {
		x, err = inst.MeasureCompressedLength(t1Trunc, "")
		if err != nil {
			return
		}
	}
	var xy uint64
	xy, err = inst.MeasureCompressedLength(t1Trunc, t2Trunc)
	if err != nil {
		return
	}
	ccc = compression.CalculateConditionalComplexityOfCompression(xy, x)
	return
}

func (inst *Analyzer) MeasureNcdProfile(texts iter.Seq[string]) (totalLength uint64, count int64, ncd float64, err error) {
	var t1Trunc, t2Trunc string
	t1Trunc, t2Trunc, count, totalLength = inst.gatherEqualLengthProfile(texts)
	x := inst.InputCompressedLen()
	if totalLength != uint64(len(inst.InputText())) {
		x, err = inst.MeasureCompressedLength(t1Trunc, "")
		if err != nil {
			return
		}
	}
	var xy, y uint64
	xy, err = inst.MeasureCompressedLength(t1Trunc, t2Trunc)
	if err != nil {
		return
	}
	y, err = inst.MeasureCompressedLength(t2Trunc, "")
	if err != nil {
		return
	}
	ncd = compression.CalculateNormalizedCompressionDistance(xy, x, y)
	return
}
