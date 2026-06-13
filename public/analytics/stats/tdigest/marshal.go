package tdigest

import (
	"encoding/json"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type stateDTO struct {
	Delta       float64   `json:"delta"`
	N           int64     `json:"n"`
	TotalWeight float64   `json:"total_weight"`
	Min         float64   `json:"min"`
	Max         float64   `json:"max"`
	Means       []float64 `json:"means"`
	Weights     []float64 `json:"weights"`
}

func (inst *TDigest) MarshalJSON() (data []byte, err error) {
	inst.compress()
	dto := stateDTO{
		Delta:       inst.delta,
		N:           inst.n,
		TotalWeight: inst.totalWeight,
		Means:       inst.means,
		Weights:     inst.weights,
	}
	if inst.n > 0 {
		dto.Min = inst.min
		dto.Max = inst.max
	}
	data, err = json.Marshal(dto)
	return
}

func (inst *TDigest) UnmarshalJSON(data []byte) (err error) {
	var dto stateDTO
	err = json.Unmarshal(data, &dto)
	if err != nil {
		err = eh.Errorf("tdigest: unmarshal: %w", err)
		return
	}
	if len(dto.Means) != len(dto.Weights) {
		err = eh.Errorf("tdigest: means/weights length mismatch (%d vs %d)", len(dto.Means), len(dto.Weights))
		return
	}
	// Reject NaN/Inf means before the order check: any comparison
	// against NaN evaluates to false, so a smuggled NaN would slip
	// past `Means[i] < Means[i-1]` and poison every future quantile.
	for i, m := range dto.Means {
		if math.IsNaN(m) || math.IsInf(m, 0) {
			err = eh.Errorf("tdigest: invalid mean at index %d: %v", i, m)
			return
		}
	}
	for i := 1; i < len(dto.Means); i++ {
		if dto.Means[i] < dto.Means[i-1] {
			err = eh.Errorf("tdigest: means not ascending at index %d (%v < %v)",
				i, dto.Means[i], dto.Means[i-1])
			return
		}
	}
	for i, w := range dto.Weights {
		if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			err = eh.Errorf("tdigest: invalid weight at index %d: %v", i, w)
			return
		}
	}
	delta := dto.Delta
	if math.IsNaN(delta) || delta < minDelta {
		delta = defaultDelta
	} else if delta > maxDelta {
		delta = maxDelta
	}
	bufCap := int32(bufMultiple * delta)
	inst.delta = delta
	inst.bufCap = bufCap
	inst.n = dto.N
	inst.totalWeight = dto.TotalWeight
	inst.means = append(inst.means[:0], dto.Means...)
	inst.weights = append(inst.weights[:0], dto.Weights...)
	inst.bufMeans = inst.bufMeans[:0]
	inst.bufWeights = inst.bufWeights[:0]
	if cap(inst.bufMeans) < int(bufCap) {
		inst.bufMeans = make([]float64, 0, bufCap)
		inst.bufWeights = make([]float64, 0, bufCap)
	}
	if inst.n == 0 {
		inst.min = math.Inf(1)
		inst.max = math.Inf(-1)
	} else {
		inst.min = dto.Min
		inst.max = dto.Max
	}
	return
}
