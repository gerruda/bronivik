package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	// Register should be safe to call multiple times
	Register()
	Register()

	// IncHTTP should not panic
	assert.NotPanics(t, func() {
		IncHTTP("test_endpoint")
	})
}
