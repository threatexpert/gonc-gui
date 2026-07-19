package receivedfile

import (
	"os"

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
		target, err := httpdownload.ResolveLocalPath(saveDir, file)
		if err == nil && !file.IsDir {
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
