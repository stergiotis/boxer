package tokenmeter

import "github.com/stergiotis/boxer/public/llm/openaichat"

// ImageTokens is what an attached image is reserved as: providers count
// images in ways a byte size does not predict.
const ImageTokens = 1000

// Estimate is the input tokens a message list is expected to use: about
// four bytes of text a token, and ImageTokens per image.
func Estimate(ms []openaichat.Message) (n int64) {
	var text int64
	for _, m := range ms {
		text += int64(len(m.Content))
		n += int64(len(m.Images)) * ImageTokens
	}
	n += text / 4
	return
}

// Used is what a request and its answer took: the provider's reported
// counts when it reported any, the estimate over the messages otherwise.
func Used(reportedIn int32, reportedOut int32, ms []openaichat.Message) (n int64) {
	if reportedIn > 0 || reportedOut > 0 {
		return int64(reportedIn) + int64(reportedOut)
	}
	return Estimate(ms)
}

// Fraction is used over the context size, 0 when the size is unknown.
func Fraction(used int64, context int64) (f float32) {
	if context <= 0 {
		return 0
	}
	return float32(used) / float32(context)
}

// Over says used reaches percent of the context size; a size of zero, the
// model's context unknown, is never reached.
func Over(used int64, context int64, percent int64) (over bool) {
	return context > 0 && used*100 >= percent*context
}
