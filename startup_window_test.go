package main

import "testing"

func TestFitStartupWindowShrinksWithoutMaximising(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		availableWidth, availableHeight int
		width, height                   int
	}{
		{"normal desktop unchanged", 1920, 1040, 0, 0},
		{"short display", 1366, 728, 980, 664},
		{"150 percent scaling logical pixels", 1280, 693, 980, 629},
		{"narrow screen", 900, 1040, 868, 680},
		{"small work area", 800, 560, 768, 496},
		{"invalid work area", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resized, centered := false, false
			fitStartupWindow(tc.availableWidth, tc.availableHeight, func(w, h int) {
				resized = true
				if w != tc.width || h != tc.height || centered {
					t.Fatalf("unexpected resize: %dx%d, centered=%v", w, h, centered)
				}
			}, func() {
				if !resized {
					t.Fatal("must resize before centering")
				}
				centered = true
			})
			if resized != (tc.width != 0) || centered != resized {
				t.Fatalf("resized=%v centered=%v", resized, centered)
			}
		})
	}
}
