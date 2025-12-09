package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer

	// Test debug level
	SetLevel(LevelDebug)
	SetOutput(&buf)
	Debug("debug message")
	output := buf.String()
	if !strings.Contains(output, "DEBUG") || !strings.Contains(output, "debug message") {
		t.Errorf("Debug message not logged at Debug level. Got: %q", output)
	}

	// Test info level (should not log debug)
	buf.Reset()
	SetLevel(LevelInfo)
	Debug("debug message 2")
	if strings.Contains(buf.String(), "debug message 2") {
		t.Error("Debug message logged at Info level (should be filtered)")
	}

	// But should log info
	Info("info message")
	if !strings.Contains(buf.String(), "INFO") || !strings.Contains(buf.String(), "info message") {
		t.Error("Info message not logged at Info level")
	}

	// Test error level (should only log errors)
	buf.Reset()
	SetLevel(LevelError)
	Info("info message 2")
	if strings.Contains(buf.String(), "info message 2") {
		t.Error("Info message logged at Error level (should be filtered)")
	}

	Error("error message")
	if !strings.Contains(buf.String(), "ERROR") || !strings.Contains(buf.String(), "error message") {
		t.Error("Error message not logged at Error level")
	}
}

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelDebug)

	Debug("debug", "key", "value")
	Info("info", "key", "value")
	Warn("warn", "key", "value")
	Error("error", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "DEBUG") {
		t.Error("Debug not logged")
	}
	if !strings.Contains(output, "INFO") {
		t.Error("Info not logged")
	}
	if !strings.Contains(output, "WARN") {
		t.Error("Warn not logged")
	}
	if !strings.Contains(output, "ERROR") {
		t.Error("Error not logged")
	}
}

func TestWith(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelInfo)

	log := With("component", "test")
	log.Info("message", "data", 123)

	output := buf.String()
	if !strings.Contains(output, "component=test") {
		t.Error("Context not preserved")
	}
	if !strings.Contains(output, "message") {
		t.Error("Message not logged")
	}
	if !strings.Contains(output, "data=123") {
		t.Error("Data not logged")
	}
}

