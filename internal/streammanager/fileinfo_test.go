package streammanager

import (
	"math"
	"testing"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  float64
		expectErr bool
	}{
		// Numeric seconds - integers
		{
			name:     "zero seconds",
			input:    "0",
			expected: 0,
		},
		{
			name:     "positive integer seconds",
			input:    "123",
			expected: 123,
		},
		{
			name:     "large integer seconds",
			input:    "3661",
			expected: 3661,
		},

		// Numeric seconds - floats
		{
			name:     "decimal seconds",
			input:    "123.456",
			expected: 123.456,
		},
		{
			name:     "zero with decimal",
			input:    "0.0",
			expected: 0.0,
		},
		{
			name:     "small decimal",
			input:    "0.123",
			expected: 0.123,
		},
		{
			name:     "large decimal",
			input:    "9999.999",
			expected: 9999.999,
		},

		// HH:MM:SS format - basic
		{
			name:     "simple time",
			input:    "01:23:45",
			expected: 1*3600 + 23*60 + 45, // 5025
		},
		{
			name:     "zero time",
			input:    "00:00:00",
			expected: 0,
		},
		{
			name:     "max time values",
			input:    "23:59:59",
			expected: 23*3600 + 59*60 + 59, // 86399
		},
		{
			name:     "single digit hour",
			input:    "1:30:45",
			expected: 1*3600 + 30*60 + 45, // 5445
		},
		{
			name:     "double digit hour",
			input:    "12:30:45",
			expected: 12*3600 + 30*60 + 45, // 45045
		},
		{
			name:     "hour over 24",
			input:    "25:30:45",
			expected: 25*3600 + 30*60 + 45, // 91845
		},

		// HH:MM:SS.mmm format - with milliseconds
		{
			name:     "time with milliseconds",
			input:    "01:23:45.123",
			expected: 1*3600 + 23*60 + 45 + 0.123, // 5025.123
		},
		{
			name:     "time with single digit millisecond",
			input:    "01:23:45.1",
			expected: 1*3600 + 23*60 + 45 + 0.1, // 5025.1
		},
		{
			name:     "time with three digit milliseconds",
			input:    "01:23:45.999",
			expected: 1*3600 + 23*60 + 45 + 0.999, // 5025.999
		},
		{
			name:     "zero time with milliseconds",
			input:    "00:00:00.500",
			expected: 0.5,
		},
		{
			name:     "time with microseconds (many digits)",
			input:    "01:23:45.123456",
			expected: 1*3600 + 23*60 + 45 + 0.123456, // 5025.123456
		},

		// Edge cases - valid
		{
			name:     "leading zeros in time",
			input:    "01:02:03",
			expected: 1*3600 + 2*60 + 3, // 3723
		},
		{
			name:     "max seconds and minutes",
			input:    "01:59:59",
			expected: 1*3600 + 59*60 + 59, // 7199
		},

		// Invalid formats - should return errors
		{
			name:      "empty string",
			input:     "",
			expectErr: true,
		},
		{
			name:      "invalid format - letters",
			input:     "abc",
			expectErr: true,
		},
		{
			name:      "invalid format - missing seconds",
			input:     "01:23",
			expectErr: true,
		},
		{
			name:      "invalid format - too many parts",
			input:     "01:23:45:67",
			expectErr: true,
		},
		{
			name:     "invalid format - negative number",
			input:    "-123",
			expected: -123, // Actually valid - negative seconds are allowed
		},
		{
			name:      "invalid format - non-numeric hour",
			input:     "ab:23:45",
			expectErr: true,
		},
		{
			name:      "invalid format - non-numeric minute",
			input:     "01:ab:45",
			expectErr: true,
		},
		{
			name:      "invalid format - non-numeric second",
			input:     "01:23:ab",
			expectErr: true,
		},
		{
			name:     "minutes value 60",
			input:    "01:60:45",
			expected: 1*3600 + 60*60 + 45, // 7245 - no validation on ranges
		},
		{
			name:     "seconds value 60",
			input:    "01:23:60",
			expected: 1*3600 + 23*60 + 60, // 5040 - no validation on ranges
		},
		{
			name:      "malformed decimal",
			input:     "123.45.67",
			expectErr: true,
		},
		{
			name:      "malformed time with extra colon",
			input:     "01:23:45:",
			expectErr: true,
		},
		{
			name:      "malformed time with extra dot",
			input:     "01:23:45.",
			expectErr: true, // Empty milliseconds part doesn't match regex
		},
		{
			name:      "space in timestamp",
			input:     "01 23 45",
			expectErr: true,
		},
		{
			name:      "special characters",
			input:     "01@23#45",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTimestamp(tt.input)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got none, result: %f", result)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Use a small epsilon for floating point comparison
			const epsilon = 1e-9
			if math.Abs(result-tt.expected) > epsilon {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestParseTimestampBoundaryConditions(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  float64
		expectErr bool
	}{
		{
			name:      "very large hour value",
			input:     "999:59:59",
			expectErr: true, // Regex only allows 1-2 digits for hours
		},
		{
			name:     "very small decimal",
			input:    "0.000001",
			expected: 0.000001,
		},
		{
			name:     "very large decimal",
			input:    "999999.999999",
			expected: 999999.999999,
		},
		{
			name:     "maximum precision milliseconds",
			input:    "00:00:01.999999999",
			expected: 1.999999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTimestamp(tt.input)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got none, result: %f", result)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			const epsilon = 1e-9
			if math.Abs(result-tt.expected) > epsilon {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}
