package badger

import (
	badger "github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

// ZerologLoggerAdapter Zero usage: opts := badger.DefaultOptions(storePath).WithLogger(&badgerutil.ZerologLoggerAdapter{})
type ZerologLoggerAdapter struct {
}

func (inst *ZerologLoggerAdapter) Errorf(s string, i ...interface{}) {
	if i != nil {
		log.Error().Interface("arguments", i).Msgf(s, i...)
	} else {
		log.Error().Msgf(s, i...)
	}
}

func (inst *ZerologLoggerAdapter) Warningf(s string, i ...interface{}) {
	if i != nil {
		log.Warn().Interface("arguments", i).Msgf(s, i...)
	} else {
		log.Warn().Msgf(s, i...)
	}
}

func (inst *ZerologLoggerAdapter) Infof(s string, i ...interface{}) {
	if i != nil {
		log.Info().Interface("arguments", i).Msgf(s, i...)
	} else {
		log.Info().Msgf(s, i...)
	}
}

func (inst *ZerologLoggerAdapter) Debugf(s string, i ...interface{}) {
	if i != nil {
		log.Debug().Interface("arguments", i).Msgf(s, i...)
	} else {
		log.Debug().Msgf(s, i...)
	}
}

var _ badger.Logger = (*ZerologLoggerAdapter)(nil)
