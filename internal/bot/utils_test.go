package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizePhone(t *testing.T) {
	b := &Bot{}

	tests := []struct {
		input    string
		expected string
	}{
		{"89991234567", "79991234567"},
		{"79991234567", "79991234567"},
		{"+7 (999) 123-45-67", "79991234567"},
		{"9991234567", "79991234567"},
		{"123", ""},
		{"abcdefghijk", ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, b.normalizePhone(tt.input))
	}
}

func TestFormatPhoneForDisplay(t *testing.T) {
	b := &Bot{}

	tests := []struct {
		input    string
		expected string
	}{
		{"79991234567", "+7 (999) 123-45-67"},
		{"9991234567", "(999) 123-45-67"},
		{"12345", "12345"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, b.formatPhoneForDisplay(tt.input))
	}
}

func TestSanitizeInput(t *testing.T) {
	b := &Bot{}

	tests := []struct {
		input    string
		expected string
	}{
		{"Hello <world>", "Hello &lt;world&gt;"},
		{"Line\nBreak", "Line Break"},
		{"  Spaces  ", "Spaces"},
		{"'Quotes'", "&#39;Quotes&#39;"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, b.sanitizeInput(tt.input))
	}
}
