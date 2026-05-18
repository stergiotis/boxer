package good

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/rs/zerolog/log"
	"lukechampine.com/blake3"
)

func use() (id string, err error) {
	_ = blake3.New(32, nil)
	log.Info().Msg("ok")
	id, err = gonanoid.New()
	return
}
