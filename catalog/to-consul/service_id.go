package catalog

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"
)

// serviceID generates a unique ID for a service. This ID is not meant
// to be particularly human-friendly.
func serviceID(name, addr string) string {
	// sha1 is fine because we're doing this for uniqueness, not any
	// cryptographic strength. We then take only the first 12 because its
	// _probably_ unique and makes it easier to read.
	rand.Seed(time.Now().UnixNano())
	sum := sha1.Sum([]byte(fmt.Sprintf("%s-%s-%d", name, addr, rand.Int())))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(sum[:])[:12])
}
