//go:build !gonc_noexec

package main

// gonc-gui uses gonc only for its network features. Build it with
// -tags gonc_noexec to disable gonc's shell server (:sh, -shellagent), shell
// client (-shell), raw terminal mode (-pty), external command execution (-e),
// and daemon mode (-daemon). The supplied scripts pass this tag. This file is compiled
// only when the tag is missing, and stops the build with:
//
//	undefined: build_gonc_gui_with_tags_gonc_noexec
var _ = build_gonc_gui_with_tags_gonc_noexec
