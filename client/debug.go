package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Severity int

const (
	DEBUG Severity = iota
	INFO
	WARN
	ERROR
)

var debugEnabled bool

var logFile *os.File

// SetDebug enables or disables debug output
func SetDebug(enabled bool) {
	debugEnabled = enabled
}

// SetLogFile sets up file logging
func SetLogFile(filename string) error {
	if filename == "" {
		return nil
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %v", err)
		}
	}

	// Close existing log file if any
	CloseLogFile()

	// Open new log file
	var err error
	logFile, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	// Write initial log entry
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	logLine := fmt.Sprintf("%s [INFO] Log file initialized\n", timestamp)
	if _, err := logFile.WriteString(logLine); err != nil {
		return fmt.Errorf("failed to write initial log entry: %v", err)
	}

	return nil
}

// CloseLogFile closes the log file if it's open
func CloseLogFile() {
	if logFile != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05.000")
		logLine := fmt.Sprintf("%s [INFO] Log file closed\n", timestamp)
		logFile.WriteString(logLine) // Best effort write, ignore errors on close
		logFile.Close()
		logFile = nil
	}
}

// Debug outputs debug messages with severity levels
func Debug(message string, severity Severity) {
	var prefix string

	switch severity {
	case DEBUG:
		if !debugEnabled {
			return
		}
		prefix = "[DEBUG]"
	case INFO:
		prefix = "[INFO]"
	case WARN:
		prefix = "[WARN]"
	case ERROR:
		prefix = "[ERROR]"
	}

	// Format the message with timestamp
	timestamp := time.Now().Format("15:04:05.000")
	formattedMsg := fmt.Sprintf("%s %s %s", timestamp, prefix, message)

	// Write to log buffer for screen display
	logBuffer.Write([]byte(formattedMsg))

	// Write to log file if enabled
	if logFile != nil {
		// Use full timestamp for file logging
		fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		logLine := fmt.Sprintf("%s %s %s\n", fullTimestamp, prefix, message)
		if _, err := logFile.WriteString(logLine); err != nil {
			// If we can't write to the log file, add that error to the log buffer
			errorMsg := fmt.Sprintf("%s [ERROR] Failed to write to log file: %v", timestamp, err)
			logBuffer.Write([]byte(errorMsg))
		}
	}
}
