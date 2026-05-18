package bad

import (
	"crypto/sha256" // want CS009 here
	"encoding/json" // want CS009 here
	"log"           // want CS009 here

	"github.com/google/uuid" // want CS009 here
)

import "log/slog" //boxer:lint disable=CS009 reason="testdata coverage of suppression"

func use() {
	_ = sha256.New()
	_, _ = json.Marshal(struct{}{})
	log.Println("x")
	_ = uuid.New()
	_ = slog.Default()
}
