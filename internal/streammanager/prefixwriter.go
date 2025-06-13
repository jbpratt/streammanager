package streammanager

import (
	"io"
	"strings"
)

// prefixWriter adds a prefix to each line written to the underlying writer
type prefixWriter struct {
	prefix string
	writer io.Writer
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	// Split the input into lines and add prefix to each
	lines := strings.Split(string(p), "\n")
	var prefixedLines []string

	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			// Don't prefix empty last line (from trailing newline)
			prefixedLines = append(prefixedLines, line)
		} else {
			prefixedLines = append(prefixedLines, pw.prefix+line)
		}
	}

	prefixedOutput := strings.Join(prefixedLines, "\n")
	_, err = pw.writer.Write([]byte(prefixedOutput))
	if err != nil {
		return 0, err
	}

	return len(prefixedOutput), nil
}
