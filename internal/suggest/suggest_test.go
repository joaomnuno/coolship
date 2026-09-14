package suggest

import "testing"

func TestClosest(t *testing.T) {
	names := []string{"work", "home", "staging"}
	for _, test := range []struct{ typed, want string }{
		{"hme", "home"},
		{"HOME", "home"},
		{"wrok", "work"},
		{"stag", "staging"},
		{"production", ""},
		{"home", ""},
		{"", ""},
	} {
		if got := Closest(test.typed, names); got != test.want {
			t.Errorf("Closest(%q) = %q, want %q", test.typed, got, test.want)
		}
	}
	if got := Closest("stauts", []string{"start", "status", "stop"}); got != "status" {
		t.Errorf("transposition: Closest(stauts) = %q", got)
	}
	if got := DidYouMean("wbe", []string{"web", "api"}); got != ` (did you mean "web"?)` {
		t.Errorf("DidYouMean = %q", got)
	}
	if got := DidYouMean("zzz", []string{"web", "api"}); got != "" {
		t.Errorf("DidYouMean far = %q", got)
	}
}
