package trail

// HttpFetch is one fetch the egress service answered or refused (ADR-0262
// §SD5). Who asked is the row's [Origin]; the agent task whose work it was,
// when there is one, its [Delegation].
type HttpFetch struct {
	_ struct{} `kind:"httpFetch"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind        string `lw:"runtimeKindHttpFetch,symbol"`
	Destination string `lw:"httpFetchDestination,symbol"`
	Purpose     string `lw:"httpFetchPurpose,symbol"`
	Sensitivity string `lw:"httpFetchSensitivity,symbol"`
	Method      string `lw:"httpFetchMethod,symbol"`
	// Url is scheme, host and path; the query is never kept.
	Url       string `lw:"httpFetchUrl,stringArray,unit"`
	Status    uint32 `lw:"httpFetchStatus,u32Array,unit"`
	Bytes     uint64 `lw:"httpFetchBytes,u64Array,unit"`
	ElapsedMs uint64 `lw:"httpFetchElapsedMs,u64Array,unit"`
	Refused   bool   `lw:"httpFetchRefused,bool"`
	// Error is the failure or the refusal reason: one element when there is one.
	Error []string `lw:"httpFetchError,stringArray"`
}
