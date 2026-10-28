package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	CorrelationID string
}

var mu sync.Mutex

func New(correlationID string) *Logger {
	return &Logger{
		CorrelationID: correlationID,
	}
}

func (l *Logger) Info(message string) {
	l.write("INFO", message)
}

func (l *Logger) Error(message string) {
	l.write("ERROR", message)
}

func (l *Logger) write(level string, message string) {
	mu.Lock()
	defer mu.Unlock()

	// Create logs directory.
	if err := os.MkdirAll("logs", 0755); err != nil {
		fmt.Println("failed to create logs directory:", err)
		return
	}

	// Create one file per day.
	filename := time.Now().UTC().Format("2006-01-02") + ".log"
	path := filepath.Join("logs", filename)

	file, err := os.OpenFile(
		path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)

	if err != nil {
		fmt.Println("failed to open log file:", err)
		return
	}

	defer file.Close()

	timestamp := time.Now().UTC().Format(time.RFC3339)

	logLine := fmt.Sprintf(
		"time=%s correlation_id=%s level=%s message=%s\n",
		timestamp,
		l.CorrelationID,
		level,
		message,
	)

	if _, err := file.WriteString(logLine); err != nil {
		fmt.Println("failed to write log:", err)
	}
}
