//go:build !windows

package filetrash

import (
	"fmt"
	"os/exec"
	"runtime"
)

func Available() bool {
	if runtime.GOOS == "darwin" {
		_, err := exec.LookPath("osascript")
		return err == nil
	}
	if runtime.GOOS == "linux" {
		_, err := exec.LookPath("gio")
		return err == nil
	}
	return false
}

func Move(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// The path is an argument, never executable AppleScript text. Foundation's
		// trash API reports failure instead of falling back to permanent deletion.
		cmd = exec.Command("osascript", "-l", "JavaScript", "-e", `ObjC.import('Foundation'); function run(args) { var error = Ref(); var url = $.NSURL.fileURLWithPath(args[0]); if (!$.NSFileManager.defaultManager.trashItemAtURLResultingItemURLError(url, null, error)) { throw Error(error[0] ? ObjC.unwrap(error[0].localizedDescription) : 'Trash unavailable'); } }`, "--", target)
	case "linux":
		cmd = exec.Command("gio", "trash", "--", target)
	default:
		return fmt.Errorf("system trash is unavailable")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("system trash failed: %w: %s", err, output)
	}
	return nil
}
