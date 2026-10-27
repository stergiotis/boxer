package ladingpublish

import (
	"strconv"

	"github.com/stergiotis/boxer/public/config/env"
)

var (
	Ceiling = env.NewCategorialString(env.Spec{
		Name:        "BOXER_LADING_PUBLISH_TIER",
		Default:     "AMBER",
		Description: "the highest TLP tier a tree published into the lading store may carry (ADR-0338); everyone who can read the store reads what is in it, and a confined tree is refused whatever this says",
		Category:    env.CategoryE("boxer-lading"),
	}, []string{"CLEAR", "GREEN", "AMBER", "AMBER+STRICT", "RED"})
	MaxTtlDays = env.NewInt(env.Spec{
		Name:        "BOXER_LADING_PUBLISH_MAX_DAYS",
		Default:     "90",
		Description: "the longest retention class, in days, a publish into the lading store may name (ADR-0338)",
		Category:    env.CategoryE("boxer-lading"),
	})
	MaxBytes = env.NewInt(env.Spec{
		Name:        "BOXER_LADING_PUBLISH_BYTES",
		Default:     strconv.Itoa(256 << 20),
		Description: "the most bytes of files one publish into the lading store may carry (ADR-0338); a larger tree is refused, not split",
		Category:    env.CategoryE("boxer-lading"),
	})
)
