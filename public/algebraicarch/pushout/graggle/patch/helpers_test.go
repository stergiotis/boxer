//go:build llm_generated_opus47

package patch

import (
	"crypto/sha256"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func ph(s string) t.PatchHash {
	return sha256.Sum256([]byte(s))
}

func nid(patchStr string, idx uint64) t.NodeID {
	return t.NodeID{Patch: ph(patchStr), Index: idx}
}