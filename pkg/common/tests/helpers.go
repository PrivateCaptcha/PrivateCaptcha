package tests

import (
	"fmt"
	randv2 "math/rand/v2"
)

func GenerateRandomIPv4() string {
	// Generate a random 32-bit integer
	ipInt := randv2.Uint32()
	// Extract each byte and format as IP address
	return fmt.Sprintf("%d.%d.%d.%d",
		(ipInt>>24)&0xFF,
		(ipInt>>16)&0xFF,
		(ipInt>>8)&0xFF,
		ipInt&0xFF)
}
