package config

import (
	"time"

	"github.com/stergiotis/boxer/public/batching/minibatch"
	"github.com/stergiotis/boxer/public/config"

	"github.com/urfave/cli/v2"
)

type MiniBatchConfig struct {
	SizeCriteria     int           `json:"sizeCriteria"`
	DurationCriteria time.Duration `json:"durationCriteria"`
	CountCriteria    int           `json:"countCriteria"`

	nValidationMessages int
	validated           bool
}

func (inst *MiniBatchConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:     nameTransf("sizeCriteria"),
			EnvVars:  []string{envVarNameTransf("MINI_BATCH_SIZE_CRITERIA")},
			Value:    inst.SizeCriteria,
			Category: "MiniBatch",
		},
		&cli.DurationFlag{
			Name:     nameTransf("durationCriteria"),
			EnvVars:  []string{envVarNameTransf("MINI_BATCH_DURATION_CRITERIA")},
			Value:    inst.DurationCriteria,
			Category: "MiniBatch",
		},
		&cli.IntFlag{
			Name:     nameTransf("countCriteria"),
			EnvVars:  []string{envVarNameTransf("MINI_BATCH_COUNT_CRITERIA")},
			Value:    inst.CountCriteria,
			Category: "MiniBatch",
		},
	}
}

func (inst *MiniBatchConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.SizeCriteria = ctx.Int(nameTransf("sizeCriteria"))
	inst.DurationCriteria = ctx.Duration(nameTransf("durationCriteria"))
	inst.CountCriteria = ctx.Int(nameTransf("countCriteria"))
	return inst.Validate(true)
}

func (inst *MiniBatchConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = 0
	inst.validated = true
	return
}

var _ config.Configer = (*MiniBatchConfig)(nil)

func NewMiniBatcher(config *MiniBatchConfig) (*minibatch.MiniBatcher, error) {
	return minibatch.NewMiniBatcher(config.SizeCriteria, config.CountCriteria, config.DurationCriteria, nil)
}
