# go-toast for gonc-gui

This is go-toast v2.0.3 (git.sr.ht/~jackmordaunt/go-toast, Unlicense OR MIT),
which Wails uses for Windows notifications. gonc-gui's `go.mod` replaces the
module with this copy.

There are two changes.

The PowerShell fallback is removed. Upstream sends a toast through the Windows
Runtime COM API and, if that fails, writes the toast to a temporary `.ps1` file
and runs it with a hidden `PowerShell -ExecutionPolicy Bypass -File`. That code
ships in every Wails app even when it never runs, and antivirus heuristics flag
the pattern. Here `pushPowershell` returns an error and the script template is
gone, so a toast that COM cannot show is simply not shown.

Toasts can be sent without a click handler. gonc-gui does not use Wails'
notifications on Windows (see `notifications_windows.go`): it registers its own
AppUserModelID with a name and icon only, and calls `wintoast.Push`. Upstream
registers the COM class that receives toast clicks on every push; here that
happens only after `SetAppData` was given a GUID, as Wails does.

- `wintoast/bind_windows.go`: `pushPowershell` returns an error, and `pushCOM`
  registers the class factory only when the app data has a GUID.
- `tmpl/tmpl.go`: no `powershell.go.tmpl` or `ScriptTemplate`.
- `examples/` is not copied.

To update, copy the new upstream release here and make the same changes.
