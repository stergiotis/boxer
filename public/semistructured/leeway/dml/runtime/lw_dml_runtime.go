package runtime

import (
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

var ErrInvalidStateTransition = eh.Errorf("invalid state transition")

// ErrSingleMembershipViolated reports an attribute closing with a membership
// count other than one on a channel its section declares single-instance
// (ADR-0213). Raised by the generated completeAttribute path, so it surfaces
// like every DML error: on CheckErrors and CommitEntity. Match with errors.Is.
var ErrSingleMembershipViolated = eh.Errorf("the schema declares exactly one membership per attribute on this channel")

// ErrFixedWidthExceeded reports a value longer than its fixed-width column
// (`sxN`) can hold. Raised by AppendFixedText, so it surfaces like every DML
// error: on CheckErrors and CommitEntity. Match with errors.Is.
var ErrFixedWidthExceeded = eh.Errorf("value exceeds the fixed width of its column")

// AppendFixedText appends v to a fixed-width text (`sxN`) column's builder
// with ClickHouse INSERT semantics: exactly width bytes append verbatim, a
// shorter value is zero-padded to width (the padding is stored content —
// ADR-0201 SD3 keeps it, so it reads back), and a longer one is refused. On
// refusal the zero value is appended in its place, so the section's builders
// stay aligned and the failure travels the entity's error collection
// (AppendError → CheckErrors) instead of panicking inside
// array.FixedSizeBinaryBuilder.Append, which insists on exactly width bytes.
func AppendFixedText(b *array.FixedSizeBinaryBuilder, v string, width int) (err error) {
	if len(v) == width {
		// Append copies before returning, so the no-copy view is safe.
		b.Append(unsafeperf.UnsafeStringToBytes(v))
		return
	}
	buf := make([]byte, width)
	if len(v) > width {
		b.Append(buf)
		return eb.Build().Int("len", len(v)).Int("width", width).Errorf("%w", ErrFixedWidthExceeded)
	}
	copy(buf, v)
	b.Append(buf)
	return
}
