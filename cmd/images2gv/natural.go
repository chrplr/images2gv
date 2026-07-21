/*
 * images2gv - Convert image sequences to .gv format
 * Copyright (C) 2026 Christophe Pallier
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package main

import "strings"

// naturalCompare orders strings so embedded numbers compare by value:
// "frame_9" sorts before "frame_10". Returns -1, 0 or 1.
//
// A plain lexicographic sort silently misorders unpadded frame numbering
// (frame_10 lands before frame_2), producing a video whose frames play out of
// sequence with nothing to indicate anything went wrong.
func naturalCompare(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		aDigit, bDigit := isDigit(a[0]), isDigit(b[0])

		if aDigit && bDigit {
			aNum, aRest := splitDigits(a)
			bNum, bRest := splitDigits(b)
			// Compare numerically without parsing: strip leading zeros, then
			// longer runs are larger, equal lengths compare lexicographically.
			// This avoids overflow on absurdly long digit runs.
			aTrim := strings.TrimLeft(aNum, "0")
			bTrim := strings.TrimLeft(bNum, "0")
			if len(aTrim) != len(bTrim) {
				return cmpInt(len(aTrim), len(bTrim))
			}
			if c := strings.Compare(aTrim, bTrim); c != 0 {
				return c
			}
			// Equal in value; break the tie on the literal so that "01" and "1"
			// never compare 0 — a comparator that calls distinct filenames
			// equal leaves the sort order undefined.
			if c := cmpInt(len(aNum), len(bNum)); c != 0 {
				return c
			}
			a, b = aRest, bRest
			continue
		}

		if aDigit != bDigit {
			// Digits sort before letters, so frame_2 precedes frame_a.
			if aDigit {
				return -1
			}
			return 1
		}

		if a[0] != b[0] {
			if a[0] < b[0] {
				return -1
			}
			return 1
		}
		a, b = a[1:], b[1:]
	}
	return cmpInt(len(a), len(b))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// splitDigits returns the leading digit run and the remainder.
func splitDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
