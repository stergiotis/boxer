package supervisor

// AppId is the conventional bus identity the host registers for the
// supervisor's bus client. Used in audit Sender fields.
const AppId = "runtime.task.supervisor"

// LogService is the Service tag the supervisor writes on every audited
// LogRow so factsstore queries can scope by service = LogService.
const LogService = "runtime.task.supervisor"

// The list-inflight request/reply subject lives on the task package
// (task.SubjectListInflight) so consumers can query the snapshot
// without importing the supervisor.
