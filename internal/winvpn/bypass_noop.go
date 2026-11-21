//go:build !darwin && (!linux || android)

package winvpn

func prepareInterfaceBypass(_ bypassSelection) (*bypassRoute, error) {
	return &bypassRoute{ready: func() (bool, error) { return true, nil }}, nil
}
