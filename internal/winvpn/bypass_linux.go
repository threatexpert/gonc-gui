//go:build linux && !android

package winvpn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func prepareInterfaceBypass(s bypassSelection) (*bypassRoute, error) {
	if net.ParseIP(s.IP).To4() == nil {
		return &bypassRoute{ready: func() (bool, error) { return true, nil }}, nil
	}
	return prepareReversePathFilter("/proc/sys/net/ipv4/conf", s)
}

func readRPFilter(data []byte, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	switch strings.TrimSpace(string(data)) {
	case "0":
		return 0, nil
	case "1":
		return 1, nil
	case "2":
		return 2, nil
	}
	return 0, fmt.Errorf("unexpected rp_filter value %q", data)
}

func prepareReversePathFilter(conf string, s bypassSelection) (*bypassRoute, error) {
	// Pin the device's proc directory so cleanup cannot write to a new
	// device that happens to reuse the same name.
	device, err := os.OpenRoot(filepath.Join(conf, s.Name))
	if err != nil {
		return nil, fmt.Errorf("open rp_filter for %s: %w", s.Name, err)
	}
	iface, err := net.InterfaceByName(s.Name)
	if err != nil || iface.Index != s.Index {
		device.Close()
		return nil, fmt.Errorf("physical interface %s changed during bypass setup", s.Name)
	}
	read := func() (int, int, error) {
		all, err := readRPFilter(os.ReadFile(filepath.Join(conf, "all", "rp_filter")))
		if err != nil {
			return 0, 0, err
		}
		local, err := readRPFilter(device.ReadFile("rp_filter"))
		return local, max(local, all), err
	}
	write := func(value int) error {
		f, err := device.OpenFile("rp_filter", os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, value)
		return errors.Join(err, f.Close())
	}
	original, effective, err := read()
	// Strict filtering drops replies on this device when the reverse route
	// points into TUN. Linux uses max(conf/all, conf/device).
	if err == nil && effective == 1 {
		err = write(2)
	}
	if err != nil {
		device.Close()
		return nil, fmt.Errorf("prepare rp_filter for %s: %w", s.Name, err)
	}
	return &bypassRoute{
		interfaceScoped: true,
		ready: func() (bool, error) {
			_, current, err := read()
			return current != 1, err
		},
		close: func() error {
			if effective == 1 {
				current, err := readRPFilter(device.ReadFile("rp_filter"))
				if err == nil && current == 2 {
					err = write(original)
				}
				if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENODEV) {
					return fmt.Errorf("restore rp_filter for %s: %w", s.Name, err)
				}
			}
			return device.Close()
		},
	}, nil
}
