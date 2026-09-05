package core

import "log"

// debugEnabled controls whether debugf actually writes output.
// It is toggled once at startup via SetDebugLogging based on cfg.LogLevel == "debug".
var debugEnabled bool

// SetDebugLogging enables or disables debug-level diagnostic logging for the core package.
func SetDebugLogging(enabled bool) {
	debugEnabled = enabled
}

// debugf writes a debug-level log line only when debug logging is enabled.
func debugf(format string, args ...any) {
	if !debugEnabled {
		return
	}
	log.Printf("[debug] "+format, args...)
}
