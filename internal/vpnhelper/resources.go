package vpnhelper

import "errors"

type networkSession interface {
	Stop() error
	DNSServers() []string
	UpdateDNS([]string) error
	UpdateProxy(string) error
}

type bypassSession interface {
	Close() error
	Refresh() (string, uint64, error)
}

type helperResources struct {
	active networkSession
	bypass bypassSession
}

// Attempt both cleanups even if one fails. Failed resources remain retryable.
func (r *helperResources) cleanup() error {
	var networkErr, bypassErr error
	if r.active != nil {
		networkErr = r.active.Stop()
		if networkErr == nil {
			r.active = nil
		}
	}
	if r.bypass != nil {
		bypassErr = r.bypass.Close()
		if bypassErr == nil {
			r.bypass = nil
		}
	}
	return errors.Join(networkErr, bypassErr)
}
