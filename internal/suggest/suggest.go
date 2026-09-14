// Package suggest finds the known name a mistyped one was probably meant to
// be, for "did you mean" hints in diagnostics.
package suggest

import (
	"fmt"
	"sort"
	"strings"
)

// maxDistance matches Cobra's default SuggestionsMinimumDistance, so a
// mistyped context or target is caught as readily as a mistyped command.
const maxDistance = 2

// Closest returns the candidate typed is most likely a misspelling of: the
// one within two edits (ignoring case, a swap of neighbors counting as one)
// with the fewest, or one typed is a prefix of. Ties go to the alphabetically first. It returns "" when nothing
// is close, or when typed already names a candidate exactly.
func Closest(typed string, candidates []string) string {
	if typed == "" {
		return ""
	}
	lower := strings.ToLower(typed)
	best, bestDistance := "", maxDistance+1
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	for _, candidate := range sorted {
		if candidate == typed {
			return ""
		}
		edits := distance(lower, strings.ToLower(candidate))
		if edits > maxDistance && strings.HasPrefix(strings.ToLower(candidate), lower) {
			edits = maxDistance
		}
		if edits < bestDistance {
			best, bestDistance = candidate, edits
		}
	}
	return best
}

// DidYouMean returns ` (did you mean "name"?)` for the closest candidate, or
// "" when none is close, ready to append to a quoted unknown name.
func DidYouMean(typed string, candidates []string) string {
	if closest := Closest(typed, candidates); closest != "" {
		return fmt.Sprintf(" (did you mean %q?)", closest)
	}
	return ""
}

// distance is the optimal string alignment distance: Levenshtein plus
// adjacent transpositions as one edit, so "stauts" is one edit from "status"
// and two from "start".
func distance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	d := make([][]int, len(ar)+1)
	for i := range d {
		d[i] = make([]int, len(br)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ar); i++ {
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ar[i-1] == br[j-2] && ar[i-2] == br[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ar)][len(br)]
}
