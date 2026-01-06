package shared

import (
	// Standard packages
	"math/rand/v2"
	"time"
)

func RandomDuration(min int, max int) time.Duration {
	seconds := rand.IntN(max-min+1) + min
	return time.Duration(seconds) * time.Second
}
