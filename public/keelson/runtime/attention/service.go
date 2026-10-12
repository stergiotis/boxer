package attention

import "time"

// Service is the host's attention service: the levels, the queue that reads
// them, and the flood limit sources admit their items through.
type Service struct {
	Levels *Levels
	Queue  *Queue
	Flood  *Flood
	// Questions are the questions apps post on a model's behalf; a source of
	// the queue.
	Questions *Questions
	// Notices are the highlights kept for the person; not a source of the
	// queue.
	Notices *Notices
	// Presence is the person's status and the away log (ADR-0328).
	Presence *Presence
}

// NewService builds the service from the environment (EscalateEnv,
// FloodStreamEnv, FloodPersonEnv, AwayIdleEnv).
func NewService() (s *Service) {
	levels := &Levels{}
	s = &Service{Levels: levels, Queue: NewQueue(levels, EscalateEnv.Get()),
		Flood: NewFlood(int(FloodStreamEnv.Get()), int(FloodPersonEnv.Get()))}
	s.Questions = NewQuestions(s.Flood)
	s.Notices = NewNotices(levels)
	s.Queue.AddSource(s.Questions)
	s.Presence = NewPresence(AwayIdleEnv.Get(), time.Now())
	s.Queue.SetPresence(s.Presence)
	s.Flood.SetOnRefuse(s.Presence.recordRefusal)
	s.Notices.SetOnKept(s.Presence.recordNotice)
	return
}
