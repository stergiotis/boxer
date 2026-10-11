package attention

// Service is the host's attention service: the levels, the queue that reads
// them, and the flood limit sources admit their items through.
type Service struct {
	Levels *Levels
	Queue  *Queue
	Flood  *Flood
	// Questions are the questions apps post on a model's behalf; a source of
	// the queue.
	Questions *Questions
}

// NewService builds the service from the environment (EscalateEnv,
// FloodStreamEnv, FloodPersonEnv).
func NewService() (s *Service) {
	levels := &Levels{}
	s = &Service{Levels: levels, Queue: NewQueue(levels, EscalateEnv.Get()),
		Flood: NewFlood(int(FloodStreamEnv.Get()), int(FloodPersonEnv.Get()))}
	s.Questions = NewQuestions(s.Flood)
	s.Queue.AddSource(s.Questions)
	return
}
