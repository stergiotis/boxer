package audit

// The review domain (ADR-0350 §SD7, proposed): a person's mark on a trail
// row, recorded as an audit event. The action is the mark; the one
// reference, of type ReviewRefTrailRow, is the marked row's facts id in
// decimal, so the marked row is the event's entity; ReviewAttrReason says
// why, and ReviewAttrRule names the exception rule the mark clears, when it
// clears one.
const (
	ReviewDomain          = "review"
	ReviewActionReviewed  = "reviewed"
	ReviewActionFlagged   = "flagged"
	ReviewActionDismissed = "dismissed"
	ReviewRefTrailRow     = "trail-row"
	ReviewAttrReason      = "reason"
	ReviewAttrRule        = "rule"
)
