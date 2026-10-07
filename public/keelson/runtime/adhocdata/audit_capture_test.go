package adhocdata

import "sync"

// auditLog is what a test sees of a service's audit records: the trail
// is the record, and the hook hands the tests a copy as each is made.
type auditLog struct {
	mu   sync.Mutex
	recs []AuditRecord
}

var auditLogs sync.Map // *Service → *auditLog

// captureAudits installs the hook that collects svc's audit records.
func captureAudits(svc *Service) {
	l := &auditLog{}
	auditLogs.Store(svc, l)
	hook := func(r AuditRecord) {
		l.mu.Lock()
		l.recs = append(l.recs, r)
		l.mu.Unlock()
	}
	svc.auditHook.Store(&hook)
}

// auditRecords are the records collected since captureAudits, oldest first.
func (inst *Service) auditRecords() (recs []AuditRecord) {
	v, ok := auditLogs.Load(inst)
	if !ok {
		panic("auditRecords without captureAudits")
	}
	l := v.(*auditLog)
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]AuditRecord(nil), l.recs...)
}
