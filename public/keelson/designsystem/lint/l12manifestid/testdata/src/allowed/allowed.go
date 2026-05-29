package allowed

import "stub"

// AllowedSpecialIds — runtime services keyed by NATS-aligned dotted name
// rather than Go import path. Each must pass without diagnostic.
const _ stub.AppIdT = "runtime.broker"
const _ stub.AppIdT = "runtime.persist"
const _ stub.AppIdT = "runtime.fs"

var _ = stub.Manifest{Id: "runtime.broker"}
var _ = stub.Manifest{Id: "runtime.persist"}
var _ = stub.Manifest{Id: "runtime.fs"}
