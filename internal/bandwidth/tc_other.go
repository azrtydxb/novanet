//go:build !linux

package bandwidth

func applyTCQdisc(ifaceName string, rateBPS uint64) error {
	return ErrNotSupported
}

func removeTCQdisc(ifaceName string) error {
	return ErrNotSupported
}
