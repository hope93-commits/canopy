package contract

import (
"encoding/binary"
)

// Lattice state key prefixes — must not conflict with base prefixes:
// 0x01 (account), 0x02 (pool), 0x07 (fee params)

var (
latticeProfilePrefix  = []byte{0x03}
latticeProtocolPrefix = []byte{0x04}
)

// KeyForProfile returns the state key for a Lattice profile by address
func KeyForProfile(address []byte) []byte {
return JoinLenPrefix(latticeProfilePrefix, address)
}

// KeyForProtocol returns the state key for a registered protocol by address
func KeyForProtocol(address []byte) []byte {
return JoinLenPrefix(latticeProtocolPrefix, address)
}

// formatU64 encodes a uint64 as big-endian bytes for lexicographic ordering
func formatU64(u uint64) []byte {
b := make([]byte, 8)
binary.BigEndian.PutUint64(b, u)
return b
}
