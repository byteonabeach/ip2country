package ip2country

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
)

// parseIP converts an IP address string into a 32-bit unsigned integer.
// It supports both standard IPv4 dot-decimal notation (e.g., "8.8.8.8")
// and integer string representation (e.g., "134744072").
func parseIP(ipStr string) (uint32, error) {
	if ip := net.ParseIP(ipStr); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return binary.BigEndian.Uint32(ip4), nil
		}
		return 0, fmt.Errorf("not an IPv4 address: %s", ipStr)
	}

	if num, err := strconv.ParseUint(ipStr, 10, 32); err == nil {
		return uint32(num), nil
	}

	return 0, fmt.Errorf("invalid IP format: %s", ipStr)
}
