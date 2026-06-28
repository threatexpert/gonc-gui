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

func (c *Client) Start(config vpnconfig.Config) error {
	return fmt.Errorf("Windows VPN helper is only supported on Windows")
}
func (c *Client) Stop() error  { return nil }
func (c *Client) Close() error { return nil }
