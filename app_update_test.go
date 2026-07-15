package main

import (
	"strings"
	"testing"
)

func TestAppExposesCheckForUpdate(t *testing.T) {
	app := NewApp(nil)
	_, err := app.CheckForUpdate("not-a-version")
	if err == nil || !strings.Contains(err.Error(), "update_invalid_manifest") {
		t.Fatalf("CheckForUpdate invalid-version error = %v", err)
	}
}
