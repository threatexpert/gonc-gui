//go:build !windows && !linux && !darwin

package vpnhelper

import (
	"context"
	"fmt"

	"gonc-gui/internal/vpnconfig"
)

type Client struct{}

func StartElevated(ctx context.Context) (*Client, error) {
	return nil, fmt.Errorf("Windows VPN helper is only supported on Windows")
}

func (c *Client) Start(config vpnconfig.Config) ([]string, error) {
	return nil, fmt.Errorf("Windows VPN helper is only supported on Windows")
}
func (c *Client) StartWithTrace(config vpnconfig.Config, trace func(string)) ([]string, error) {
	return c.Start(config)
}
func (c *Client) Stop() error  { return nil }
func (c *Client) Close() error { return nil }
func (c *Client) UpdateDNS([]string) ([]string, error) {
	return nil, fmt.Errorf("unsupported platform")
}
