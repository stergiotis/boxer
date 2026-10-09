//go:build boxer_unattended

package agent

// Unattended reports that this binary was built with the unattended mode
// (build tag boxer_unattended): UnattendedEnv may then turn it on.
const Unattended = true
