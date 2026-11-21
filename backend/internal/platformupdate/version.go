package platformupdate

import (
	"cmp"
	"strconv"
	"strings"
)

func productVersion(value string) ([3]int, bool) {
	var parts [3]int
	segments := strings.Split(value, ".")
	if len(segments) != len(parts) {
		return parts, false
	}
	for i, segment := range segments {
		number, err := strconv.Atoi(segment)
		if err != nil || number < 0 || strconv.Itoa(number) != segment {
			return parts, false
		}
		parts[i] = number
	}
	return parts, true
}

func compareProductVersions(left, right string) int {
	a, aOK := productVersion(left)
	b, bOK := productVersion(right)
	if aOK != bOK {
		if aOK {
			return 1
		}
		return -1
	}
	if !aOK {
		return 0
	}
	for i := range a {
		if order := cmp.Compare(a[i], b[i]); order != 0 {
			return order
		}
	}
	return 0
}

func newerProductVersion(candidate, current string) bool {
	_, known := productVersion(current)
	return known && compareProductVersions(candidate, current) > 0
}
