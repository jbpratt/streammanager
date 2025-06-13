package streammanager

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// mockWriter is a test helper that can simulate write errors
type mockWriter struct {
	buffer     *bytes.Buffer
	failAfter  int
	writeCount int
}

func (mw *mockWriter) Write(p []byte) (n int, err error) {
	mw.writeCount++
	if mw.failAfter >= 0 && mw.writeCount > mw.failAfter {
		return 0, errors.New("mock write error")
	}
	return mw.buffer.Write(p)
}

func TestPrefixWriter_Write(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		input          string
		expectedOutput string
		expectedN      int
	}{
		{
			name:           "single line without newline",
			prefix:         "[PREFIX] ",
			input:          "hello world",
			expectedOutput: "[PREFIX] hello world",
			expectedN:      20,
		},
		{
			name:           "single line with newline",
			prefix:         "[LOG] ",
			input:          "message\n",
			expectedOutput: "[LOG] message\n",
			expectedN:      14,
		},
		{
			name:           "multiple lines",
			prefix:         "> ",
			input:          "line1\nline2\nline3",
			expectedOutput: "> line1\n> line2\n> line3",
			expectedN:      23,
		},
		{
			name:           "multiple lines with trailing newline",
			prefix:         ">> ",
			input:          "first\nsecond\n",
			expectedOutput: ">> first\n>> second\n",
			expectedN:      19,
		},
		{
			name:           "empty line in middle",
			prefix:         "* ",
			input:          "line1\n\nline3",
			expectedOutput: "* line1\n* \n* line3",
			expectedN:      18,
		},
		{
			name:           "only newlines",
			prefix:         "- ",
			input:          "\n\n\n",
			expectedOutput: "- \n- \n- \n",
			expectedN:      9,
		},
		{
			name:           "empty input",
			prefix:         "[EMPTY] ",
			input:          "",
			expectedOutput: "",
			expectedN:      0,
		},
		{
			name:           "only trailing newline",
			prefix:         "# ",
			input:          "\n",
			expectedOutput: "# \n",
			expectedN:      3,
		},
		{
			name:           "no prefix",
			prefix:         "",
			input:          "line1\nline2\n",
			expectedOutput: "line1\nline2\n",
			expectedN:      12,
		},
		{
			name:           "complex multiline with empty lines",
			prefix:         "[INFO] ",
			input:          "start\n\nmiddle\n\nend\n",
			expectedOutput: "[INFO] start\n[INFO] \n[INFO] middle\n[INFO] \n[INFO] end\n",
			expectedN:      54,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			pw := &prefixWriter{
				prefix: tt.prefix,
				writer: &buf,
			}

			n, err := pw.Write([]byte(tt.input))
			if err != nil {
				t.Errorf("Write() error = %v, want nil", err)
				return
			}

			if n != tt.expectedN {
				t.Errorf("Write() returned n = %d, want %d", n, tt.expectedN)
			}

			output := buf.String()
			if output != tt.expectedOutput {
				t.Errorf("Write() output = %q, want %q", output, tt.expectedOutput)
			}
		})
	}
}

func TestPrefixWriter_WriteError(t *testing.T) {
	tests := []struct {
		name      string
		failAfter int
		input     string
	}{
		{
			name:      "immediate write failure",
			failAfter: 0,
			input:     "test data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockW := &mockWriter{
				buffer:    &bytes.Buffer{},
				failAfter: tt.failAfter,
			}

			pw := &prefixWriter{
				prefix: "[TEST] ",
				writer: mockW,
			}

			n, err := pw.Write([]byte(tt.input))

			if err == nil {
				t.Error("Write() error = nil, want error")
				return
			}

			if n != 0 {
				t.Errorf("Write() returned n = %d, want 0 on error", n)
			}

			if !strings.Contains(err.Error(), "mock write error") {
				t.Errorf("Write() error = %v, want mock write error", err)
			}
		})
	}
}

func TestPrefixWriter_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := &prefixWriter{
		prefix: "[MULTI] ",
		writer: &buf,
	}

	// First write
	n1, err1 := pw.Write([]byte("first line\n"))
	if err1 != nil {
		t.Fatalf("First write failed: %v", err1)
	}

	// Second write
	n2, err2 := pw.Write([]byte("second line\n"))
	if err2 != nil {
		t.Fatalf("Second write failed: %v", err2)
	}

	// Third write without newline
	n3, err3 := pw.Write([]byte("third line"))
	if err3 != nil {
		t.Fatalf("Third write failed: %v", err3)
	}

	expectedOutput := "[MULTI] first line\n[MULTI] second line\n[MULTI] third line"
	output := buf.String()

	if output != expectedOutput {
		t.Errorf("Multiple writes output = %q, want %q", output, expectedOutput)
	}

	expectedTotalN := len(expectedOutput)
	totalN := n1 + n2 + n3

	if totalN != expectedTotalN {
		t.Errorf("Total bytes written = %d, want %d", totalN, expectedTotalN)
	}
}

func TestPrefixWriter_LargeInput(t *testing.T) {
	var buf bytes.Buffer
	pw := &prefixWriter{
		prefix: ">> ",
		writer: &buf,
	}

	// Create a large input with many lines
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "line " + string(rune('A'+i%26))
	}
	input := strings.Join(lines, "\n") + "\n"

	n, err := pw.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write failed for large input: %v", err)
	}

	output := buf.String()
	outputLines := strings.Split(output, "\n")

	// Should have 1000 prefixed lines plus one empty line from trailing newline
	if len(outputLines) != 1001 {
		t.Errorf("Expected 1001 output lines, got %d", len(outputLines))
	}

	// Check that each non-empty line has the prefix
	for i, line := range outputLines[:1000] {
		if !strings.HasPrefix(line, ">> ") {
			t.Errorf("Line %d missing prefix: %q", i, line)
			break
		}
	}

	// Check byte count
	if n != len(output) {
		t.Errorf("Returned byte count %d doesn't match output length %d", n, len(output))
	}
}

func TestPrefixWriter_EdgeCases(t *testing.T) {
	t.Run("nil underlying writer panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with nil writer")
			}
		}()

		pw := &prefixWriter{
			prefix: "test",
			writer: nil,
		}
		pw.Write([]byte("test"))
	})

	t.Run("very long prefix", func(t *testing.T) {
		var buf bytes.Buffer
		longPrefix := strings.Repeat("PREFIX", 100) + " "
		pw := &prefixWriter{
			prefix: longPrefix,
			writer: &buf,
		}

		input := "short line\n"
		n, err := pw.Write([]byte(input))
		if err != nil {
			t.Errorf("Write failed with long prefix: %v", err)
		}

		expected := longPrefix + "short line\n"
		if buf.String() != expected {
			t.Error("Long prefix not applied correctly")
		}

		if n != len(expected) {
			t.Errorf("Byte count mismatch: got %d, want %d", n, len(expected))
		}
	})
}

// Benchmark tests
func BenchmarkPrefixWriter_SingleLine(b *testing.B) {
	var buf bytes.Buffer
	pw := &prefixWriter{
		prefix: "[BENCH] ",
		writer: &buf,
	}
	data := []byte("benchmark test line\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		pw.Write(data)
	}
}

func BenchmarkPrefixWriter_MultipleLines(b *testing.B) {
	var buf bytes.Buffer
	pw := &prefixWriter{
		prefix: "[BENCH] ",
		writer: &buf,
	}
	data := []byte("line1\nline2\nline3\nline4\nline5\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		pw.Write(data)
	}
}
