package discovery

import (
	"crypto/rand"
	"fmt"
)

// newRequestID returns a UUID-like string (e.g. "uuid-1234" style) for request_id.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", b[0])
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
