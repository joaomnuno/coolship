package ui

import (
	"fmt"
	"time"
)

// FormatElapsed formats a duration the way every Coolship view shows one:
// m:ss, or h:mm:ss from an hour on, in whole seconds (0:23, 1:05, 1:02:03).
// A negative duration is shown as 0:00.
func FormatElapsed(d time.Duration) string {
	d = max(d, 0).Truncate(time.Second)
	hours, minutes, seconds := int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
