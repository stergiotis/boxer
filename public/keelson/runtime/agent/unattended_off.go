//go:build !boxer_unattended

package agent

// Unattended reports that this binary was built with the unattended mode
// (build tag boxer_unattended): UnattendedEnv may then turn it on. Without
// the tag it is false and the mode's code paths compile out.
const Unattended = false
