package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const maxLogFiles = 5

// Setup creates a log file for the current run and prunes old ones.
// Returns an io.Writer that writes to the log file, and a cleanup function.
// The log directory is <configDir>/localsync/logs/<binary>/.
// Log files are named with a timestamp: 2006-01-02_15-04-05.log
func Setup(binary string) (io.Writer, func(), error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot determine config dir: %w", err)
	}

	logDir := filepath.Join(dir, "localsync", "logs", binary)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("cannot create log dir: %w", err)
	}

	// Prune old log files (keep last maxLogFiles - 1 to make room for the new one)
	pruneLogFiles(logDir, maxLogFiles-1)

	filename := time.Now().Format("2006-01-02_15-04-05") + ".log"
	path := filepath.Join(logDir, filename)
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create log file: %w", err)
	}

	cleanup := func() { f.Close() }
	return f, cleanup, nil
}

func pruneLogFiles(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var logFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".log" {
			logFiles = append(logFiles, e.Name())
		}
	}

	// Sort alphabetically (timestamp format sorts chronologically)
	sort.Strings(logFiles)

	// Remove oldest files beyond the keep limit
	if len(logFiles) > keep {
		for _, name := range logFiles[:len(logFiles)-keep] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
