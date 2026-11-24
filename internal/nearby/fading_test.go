package nearby

import (
	"testing"
	"time"
)

func TestSilentDeviceFadesThenLeaves(t *testing.T) {
	s := bareService(t)
	irk, _ := NewIRK()
	tag, _ := makeTag(irk)
	start := time.Now()
	id, _, changed := s.observeLocked(&advert{Tag: tag, Name: "Laptop", Port: 19732}, "192.168.1.5", nil, start)
	if !changed || s.devices[id].Status != DeviceOnline {
		t.Fatalf("new device: changed=%v status=%q, want online", changed, s.devices[id].Status)
	}

	// Ordinary gaps between announcements change nothing.
	if s.pruneLocked(start.Add(DeviceFadeAfter)) {
		t.Fatal("a device heard from within DeviceFadeAfter changed")
	}
	if !s.pruneLocked(start.Add(DeviceFadeAfter + time.Second)) {
		t.Fatal("a silent device was not reported as changed")
	}
	if got := s.devices[id].Status; got != DeviceFading {
		t.Fatalf("status %q, want fading", got)
	}
	if s.pruneLocked(start.Add(DeviceFadeAfter + 2*time.Second)) {
		t.Fatal("an already fading device changed again")
	}

	// Hearing from it again brings it back.
	back := start.Add(DeviceFadeAfter + 3*time.Second)
	if _, _, changed := s.observeLocked(&advert{Tag: tag, Name: "Laptop", Port: 19732}, "192.168.1.5", nil, back); !changed {
		t.Fatal("a fading device that announced again was not reported as changed")
	}
	if got := s.devices[id].Status; got != DeviceOnline {
		t.Fatalf("status %q after an announcement, want online", got)
	}
	if _, _, changed := s.observeLocked(&advert{Tag: tag, Name: "Laptop", Port: 19732}, "192.168.1.5", nil, back.Add(time.Second)); changed {
		t.Fatal("a repeated announcement from an online device was reported as changed")
	}

	if !s.pruneLocked(back.Add(time.Second + DeviceExpiry + time.Millisecond)) {
		t.Fatal("an expired device was not reported as changed")
	}
	if _, ok := s.devices[id]; ok {
		t.Fatal("an expired device is still listed")
	}
}
