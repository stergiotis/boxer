package good

// A copy goes to the broker as a request on the bus; the opcode is the
// host's to build from what the broker queued.
type bus interface {
	Request(subject string, payload []byte) ([]byte, error)
}

func copyIt(b bus, text string) {
	_, _ = b.Request("clipboard.write", []byte(text))
}
