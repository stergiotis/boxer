package config

import (
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/ea"
	ea2 "github.com/stergiotis/boxer/public/fec/ea"
	"github.com/stergiotis/boxer/public/fec/ea/golay24"
	"github.com/stergiotis/boxer/public/fec/ea/passthrough"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"

	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
)

type AlgorithmE uint16

const (
	AlgorithmPassthrough AlgorithmE = 0
	AlgorithmGolay24     AlgorithmE = 1
	AlgorithmMax         AlgorithmE = 1
)

var AlgorithmToString = [AlgorithmMax + 1]string{
	"passthrough",
	"golay24",
}

var AlgorithmResolveFunc = config.GenerateResolverFunc(AlgorithmToString[:], true)

type FecConfig struct {
	FecAlgorithm                    uint16 `json:"algorithm"`
	NAnchorBytes                    uint8  `json:"nAnchorBytes"`
	AnchorMaxHammingDistPerByteIncl uint8  `json:"anchorMaxHammingDistPerByteIncl"`
	MaxMessageSize                  uint32 `json:"maxMessageSize"`
	validated                       bool
	nValidationMessages             int
}

func (inst *FecConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     nameTransf("fecAlgorithm"),
			Required: false,
			Value:    AlgorithmToString[inst.FecAlgorithm],
		},
		&cli.UintFlag{
			Name:     nameTransf("nAnchorBytes"),
			Required: false,
			Value:    uint(inst.NAnchorBytes),
		},
		&cli.UintFlag{
			Name:     nameTransf("anchorMaxHammingDistPerByteIncl"),
			Required: false,
			Value:    uint(inst.AnchorMaxHammingDistPerByteIncl),
			Action: func(context *cli.Context, u uint) error {
				if u > 24 {
					return eh.Errorf("out of range: can not have more than 24 bit errors per 24 bits")
				}
				return nil
			},
		},
		&cli.UintFlag{
			Name:     nameTransf("maxMessageSize"),
			Required: inst.MaxMessageSize == 0,
			Value:    uint(inst.MaxMessageSize),
		},
	}
}

func (inst *FecConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	{
		a, err := AlgorithmResolveFunc(ctx.String(nameTransf("fecAlgorithm")))
		if err != nil {
			log.Error().Err(err).Msg("invalid fecAlgorithm")
			nMessages++
			inst.FecAlgorithm = 0
		} else {
			inst.FecAlgorithm = uint16(a)
		}
	}
	inst.NAnchorBytes = uint8(ctx.Uint(nameTransf("nAnchorBytes")))
	inst.AnchorMaxHammingDistPerByteIncl = uint8(ctx.Uint(nameTransf("anchorMaxHammingDistIncl")))
	inst.MaxMessageSize = uint32(ctx.Uint(nameTransf("maxMessageSize")))
	return inst.Validate(true)
}

func (inst *FecConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	if inst.FecAlgorithm > uint16(AlgorithmMax) {
		log.Error().Uint16("fecAlgorithm", inst.FecAlgorithm).Uint16("max", uint16(AlgorithmMax)).Msg("fecAlgorithm is out of range")
		nMessages++
	}
	if inst.AnchorMaxHammingDistPerByteIncl > 24 {
		log.Error().Uint8("value", inst.AnchorMaxHammingDistPerByteIncl).Msg("out of range: can not have more than 24 bit errors per 24 bits")
		nMessages++
	}
	if inst.MaxMessageSize < 1 {
		log.Error().Uint32("value", inst.MaxMessageSize).Msg("unreasonably small message size")
		nMessages++
	}
	inst.validated = true
	inst.nValidationMessages = nMessages
	return
}

var _ config.Configer = (*FecConfig)(nil)

func NewWriterFromConfig(w io.Writer, config *FecConfig) (ea2.MessageWriter, error) {
	if config.Validate(false) > 0 {
		return nil, eh.Errorf("invalid fec config")
	}
	switch AlgorithmE(config.FecAlgorithm) {
	case AlgorithmPassthrough:
		return passthrough.NewWriter(w, config.NAnchorBytes), nil
	case AlgorithmGolay24:
		return golay24.NewWriter(w, config.NAnchorBytes), nil
	}
	return nil, eh.Errorf("unimplemented algorithm %d", config.FecAlgorithm)
}

func NewReaderFromConfig(r ea.ByteBlockDiscardReader, config *FecConfig) (ea2.MessageReader, error) {
	if config.Validate(false) > 0 {
		return nil, eh.Errorf("invalid fec config")
	}
	switch AlgorithmE(config.FecAlgorithm) {
	case AlgorithmPassthrough:
		return passthrough.NewPassthroughReader(r, config.NAnchorBytes, config.AnchorMaxHammingDistPerByteIncl, config.MaxMessageSize), nil
	case AlgorithmGolay24:
		return golay24.NewGolay24Reader(r, config.NAnchorBytes, config.AnchorMaxHammingDistPerByteIncl, config.MaxMessageSize), nil
	}
	return nil, eh.Errorf("unimplemented algorithm %d", config.FecAlgorithm)
}
