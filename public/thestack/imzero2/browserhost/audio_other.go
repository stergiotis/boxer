//go:build !wasip1

package browserhost

// AudioLane has no page to reach natively; see audio_wasip1.go for the tab.
func AudioLane(receive func(msg []byte)) (send func(msg []byte), ok bool) {
	return func([]byte) {}, false
}
