//go:build !gonc_noexec

package mobilegonc

// The Android binding uses gonc only for its network features. Build it with
// -tags gonc_noexec so gonc's shell, external commands (-e) and -daemon are
// left out: android/update-mobilegonc-aar.bat and the release workflow do.
// This file is compiled only when the tag is missing, and stops the build
// with:
//
//	undefined: build_gonc_gui_with_tags_gonc_noexec
var _ = build_gonc_gui_with_tags_gonc_noexec
