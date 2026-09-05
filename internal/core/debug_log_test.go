package core

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestDebugf(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() {
		SetDebugLogging(false)
		log.SetOutput(os.Stderr)
	})

	SetDebugLogging(false)
	debugf("test message %d", 1)
	if buf.Len() != 0 {
		t.Fatalf("debugf wrote output while disabled: %q", buf.String())
	}

	SetDebugLogging(true)
	debugf("test message %d", 1)
	got := buf.String()
	if !strings.Contains(got, "[debug] ") {
		t.Fatalf("debugf output missing debug prefix: %q", got)
	}
	if !strings.Contains(got, "test message 1") {
		t.Fatalf("debugf output missing formatted message: %q", got)
	}
}
