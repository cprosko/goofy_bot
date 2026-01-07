package shared

import (
	// Standard packages ---------------------------------------------------------

	// Allows choosing random times on an interval
	"math/rand/v2"
	// Provides time.Duration for specifying real times
	"time"
)

// RandomDuration returns a random Duration between min and max seconds (incl.)
func RandomDuration(min int, max int) time.Duration {
	seconds := rand.IntN(max-min+1) + min
	return time.Duration(seconds) * time.Second
}
