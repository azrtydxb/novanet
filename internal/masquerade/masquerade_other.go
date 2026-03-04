//go:build !linux

// Package masquerade provides SNAT masquerade rule management.
package masquerade

import "errors"

// errMasqueradeUnsupported indicates that masquerade is not available on this platform.
var errMasqueradeUnsupported = errors.New("masquerade not supported on this platform")

// EnsureMasquerade is not supported on non-Linux platforms.
func EnsureMasquerade(_, _ string) error {
	return errMasqueradeUnsupported
}
