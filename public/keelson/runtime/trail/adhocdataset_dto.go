package trail

// AdhocDataset is one operation on an ad-hoc bundle and its outcome
// (ADR-0288 (proposed) §SD5): a publish, republish, retract, a withdrawal
// when the publishing window closed, a resolve, or a refusal. Who asked is
// the row's [Origin]. When an agent's call caused it, the [Delegation],
// [Conversation] and [Cause] are what the dispatcher recorded for that call
// and confirmed to the dataset service — Attested says so; a context the
// dispatcher did not confirm refuses the operation and is not written as
// one.
//
// LocalNames, Aliases, Handles, Rows, Bytes and StreamDigests run index
// for index over the bundle's datasets. A stream digest is over the Arrow
// IPC stream as sealed, which is what every reader of the dataset reads;
// Bytes are those streams' lengths.
type AdhocDataset struct {
	_ struct{} `kind:"adhocDataset"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAdhocDataset,symbol"`

	Operation string `lw:"adhocDatasetOperation,symbol"`
	Outcome   string `lw:"adhocDatasetOutcome,symbol"`
	// Reason is the refusal or failure: one element when there is one.
	Reason []string `lw:"adhocDatasetReason,stringArray"`

	Bundle        string `lw:"adhocDatasetBundle,symbol"`
	Revision      uint64 `lw:"adhocDatasetRevision,u64Array,unit"`
	OwnerApp      string `lw:"adhocDatasetOwnerApp,symbol"`
	OwnerInstance uint64 `lw:"adhocDatasetOwnerInstance,u64Array,unit"`

	LocalNames     []string `lw:"adhocDatasetLocalNames,stringArray"`
	Aliases        []string `lw:"adhocDatasetAliases,stringArray"`
	Handles        []string `lw:"adhocDatasetHandles,stringArray"`
	Rows           []uint64 `lw:"adhocDatasetRows,u64Array"`
	Bytes          []uint64 `lw:"adhocDatasetBytes,u64Array"`
	StreamDigests  []string `lw:"adhocDatasetStreamDigests,stringArray"`
	DocumentDigest string   `lw:"adhocDatasetDocumentDigest,stringArray,unit"`

	Attested bool `lw:"adhocDatasetAttested,bool"`
	// InFlight says the attested call had not been answered when the work
	// was done; false with Attested is work done later under a call the
	// window had answered, while its task stayed live.
	InFlight bool `lw:"adhocDatasetInFlight,bool"`

	// On a publish and a republish, where the rows came from and what they
	// are (ADR-0288 (proposed) §SD5), so a bundle is described — and its
	// document reconstructible — from the trail alone: the document as
	// published; the statement that produced the rows, as the publisher
	// ran it; the datasets it read, with the alias and digest the service
	// held for each; and every dataset's columns, ColumnDatasets naming the
	// dataset by its position in LocalNames, each with the summary
	// statistics that are not values of the data: its nulls and an
	// estimated distinct count. Minimum, maximum and sample are values of
	// sealed data and stay with the dataset.
	Document       string   `lw:"adhocDatasetDocument,stringArray,unit"`
	SourceSql      string   `lw:"adhocDatasetSourceSql,stringArray,unit"`
	InputHandles   []string `lw:"adhocDatasetInputHandles,stringArray"`
	InputAliases   []string `lw:"adhocDatasetInputAliases,stringArray"`
	InputDigests   []string `lw:"adhocDatasetInputDigests,stringArray"`
	ColumnDatasets []uint32 `lw:"adhocDatasetColumnDatasets,u32Array"`
	ColumnNames    []string `lw:"adhocDatasetColumnNames,stringArray"`
	ColumnTypes    []string `lw:"adhocDatasetColumnTypes,stringArray"`
	ColumnNulls    []uint64 `lw:"adhocDatasetColumnNulls,u64Array"`
	ColumnDistinct []uint64 `lw:"adhocDatasetColumnDistinct,u64Array"`
}
