package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteStatusJSON writes StatusOutput as pretty JSON using atomic replace semantics.
func WriteStatusJSON(status StatusOutput, destPath string) error {
	dir := filepath.Dir(destPath)
	pattern := filepath.Base(destPath) + ".tmp-*"

	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(status); err != nil {
		_ = tmpFile.Close()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	removeTmp = false

	if err := os.Chmod(destPath, 0600); err != nil {
		return err
	}

	return nil
}
