package watchbillstore

// Event is one transition of a job (ADR-0223 §SD2): written after the job
// row's update was read back as won, never before. ID is the job's id; the
// envelope ts is the transition instant. Error is the boxer error chain as
// `eh.MarshalError` renders it, the shape errorview reads, and is empty on
// every transition that is not a failure.
type Event struct {
	_         struct{} `kind:"watchbillEvent"`
	ID        string   `lw:",id"`
	State     string   `lw:"watchbillEventState,eventState"`
	Attempt   uint32   `lw:"watchbillEventAttempt,eventAttempt"`
	WorkerRun string   `lw:"watchbillEventWorkerRun,eventWorkerRun"`
	Error     []byte   `lw:"watchbillEventError,eventError"`
	Note      string   `lw:"watchbillEventNote,eventNote"`
}
