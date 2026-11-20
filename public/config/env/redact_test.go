package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://localhost:8123/", "http://localhost:8123/"},
		{"plain value", "plain value"},
		{"", ""},
		{"http://alice:s3cret@ch.example:8123/", "http://alice:xxxxx@ch.example:8123/"},
		{"nats://bob:hunter2@nats.example:4222", "nats://bob:xxxxx@nats.example:4222"},
		{"http://alice@ch.example:8123/", "http://alice@ch.example:8123/"},
		{"http://ch.example:8123/?user=alice&password=s3cret", "http://ch.example:8123/?password=xxxxx&user=alice"},
		{"http://ch.example:8123/?database=x", "http://ch.example:8123/?database=x"},
		{"http://a:s3cret@[bad", unparsableRedacted},
		{"http://a:s3cret@ch.example/?z=1&a=2", "http://a:xxxxx@ch.example/?z=1&a=2"},
	} {
		got := RedactURL(tc.in)
		assert.Equal(t, tc.want, got, tc.in)
		assert.NotContains(t, got, "s3cret")
		assert.NotContains(t, got, "hunter2")
	}
}

func TestFormatValueRedactsURLCredentials(t *testing.T) {
	assert.Equal(t, "http://alice:xxxxx@ch.example/", FormatValue(Spec{}, "http://alice:s3cret@ch.example/"))
	assert.Equal(t, "<redacted>", FormatValue(Spec{Sensitive: true}, "http://alice:s3cret@ch.example/"))
}
