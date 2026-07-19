package httpdownload

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func ResolveLocalPath(saveDir string, file FileInfo) (string, error) {
	root, err := filepath.Abs(saveDir)
	if err != nil {
		return "", err
	}
	remotePath := strings.TrimPrefix(path.Clean(file.Path), "/")
	if remotePath == "." || remotePath == "" {
		remotePath = safeLocalFilename(file.Name)
	}
	if remotePath == "" {
		return "", errors.New("remote file has no safe local name")
	}
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(remotePath)))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("remote path escapes save directory: %s", file.Path)
	}
	return target, nil
}
