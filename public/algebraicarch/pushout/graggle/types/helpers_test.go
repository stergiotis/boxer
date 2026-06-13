package types

import "crypto/sha256"

// ph creates a PatchHash from a string for testing.
func ph(s string) PatchHash {
	return sha256.Sum256([]byte(s))
}

// nid creates a NodeID from a patch string and index for testing.
func nid(patchStr string, idx uint64) NodeID {
	return NodeID{Patch: ph(patchStr), Index: idx}
}
