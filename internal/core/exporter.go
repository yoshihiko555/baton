package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteStatusJSON は StatusOutput を整形 JSON で書き出す。
// 一時ファイルへ書いてから rename で置換し、破損中間状態を避ける。
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

	// 出力ファイルは読み書き権限を所有者のみに限定する。
	if err := os.Chmod(destPath, 0600); err != nil {
		return err
	}

	return nil
}
