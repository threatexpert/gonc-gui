package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"gonc-gui/internal/goncrunner"
)

func TestTagClientP2PReportPreservesPayloadAndRunID(t *testing.T) {
	report := goncrunner.P2PStatusReport{Status: "connected", Side: "send", Topic: "peer-1"}
	tagged := tagClientP2PReport(42, report)
	data, err := json.Marshal(tagged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"clientRunId":42`)) || tagged.Status != "connected" {
		t.Fatalf("tagged report = %s", data)
	}
}

func TestTransferClientRunIDIsRequiredOnlyForFileModes(t *testing.T) {
	for _, mode := range []goncrunner.Mode{goncrunner.ModeSend, goncrunner.ModeReceive} {
		err := validateTransferClientRunID(mode, 0)
		if err == nil || !strings.Contains(err.Error(), "client run ID") {
			t.Fatalf("mode %s error = %v", mode, err)
		}
	}
	if err := validateTransferClientRunID(goncrunner.ModeVPNClient, 0); err != nil {
		t.Fatalf("VPN validation error = %v", err)
	}
}
