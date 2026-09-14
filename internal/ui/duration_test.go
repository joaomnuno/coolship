package ui

import (
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                              "0:00",
		-time.Second:                   "0:00",
		1900 * time.Millisecond:        "0:01",
		23 * time.Second:               "0:23",
		time.Minute + 5*time.Second:    "1:05",
		12*time.Minute + 3*time.Second: "12:03",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
		25*time.Hour + 59*time.Second:             "25:00:59",
		59*time.Minute + 59*time.Second:           "59:59",
		61*time.Minute + 500*time.Millisecond:     "1:01:00",
	} {
		if got := FormatElapsed(d); got != want {
			t.Errorf("FormatElapsed(%v) = %q, want %q", d, got, want)
		}
	}
}
