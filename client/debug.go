package main

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

type Severity int

const (
    DEBUG Severity = iota
    INFO
    WARN
    ERROR
)

var debugEnabled bool

// SetDebug enables or disables debug output
func SetDebug(enabled bool) {
    debugEnabled = enabled
}

// Debug outputs debug messages with severity levels and colors
func Debug(message string, severity Severity) {
    var colorFunc func(format string, a ...interface{}) string
    var prefix string

    switch severity {
    case DEBUG:
        if !debugEnabled {
            return
        }
        colorFunc = color.New(color.Faint).SprintfFunc()
        prefix = "[DEBUG]"
    case INFO:
        colorFunc = color.New(color.FgGreen).SprintfFunc()
        prefix = "[INFO]"
    case WARN:
        colorFunc = color.New(color.FgYellow).SprintfFunc()
        prefix = "[WARN]"
    case ERROR:
        colorFunc = color.New(color.FgRed).SprintfFunc()
        prefix = "[ERROR]"
    }

    fmt.Fprintln(os.Stderr,colorFunc("%s %s", prefix, message))
}
