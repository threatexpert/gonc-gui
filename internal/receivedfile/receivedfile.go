package receivedfile

import (
	"os"
	"path/filepath"
	"strings"

	"gonc-gui/internal/httpdownload"
)

type State struct {
	RemotePath string `json:"remotePath"`
	Available  bool   `json:"available"`
}

func Check(saveDir string, files []httpdownload.FileInfo) []State {
	states := make([]State, 0, len(files))
	for _, file := range files {
		state := State{RemotePath: file.Path}
		target, contained := resolveContainedPath(saveDir, file)
		if contained && !file.IsDir {
			if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() && info.Size() == file.Size {
				handle, openErr := os.Open(target)
				if openErr == nil {
					state.Available = true
					_ = handle.Close()
				}
			}
		}
		states = append(states, state)
	}
	return states
}

func resolveContainedPath(saveDir string, file httpdownload.FileInfo) (string, bool) {
	root, err := filepath.Abs(saveDir)
	if err != nil {
		return "", false
	}
	target, err := httpdownload.ResolveLocalPath(saveDir, file)
	if err != nil {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil {
		return "", false
	}
	contained := rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	return realTarget, contained
}
