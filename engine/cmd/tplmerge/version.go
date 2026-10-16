package main

import (
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// highestContract is the higher of two contract versions, written MAJOR.MINOR
func highestContract(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	pa, pb := contractParts(a), contractParts(b)
	if pa[0] != pb[0] {
		if pa[0] > pb[0] {
			return a
		}
		return b
	}
	if pb[1] > pa[1] {
		return b
	}
	return a
}

func contractParts(s string) [2]int {
	var out [2]int
	parts := strings.SplitN(s, ".", 2)
	for i := range parts {
		if i >= 2 {
			break
		}
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// highestMinEngine is the higher of two engine semvers, either of which may be
// empty for no constraint. Empty is lowest, since it imposes no floor
func highestMinEngine(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case semver.Compare(canonicalSemver(b), canonicalSemver(a)) > 0:
		return b
	default:
		return a
	}
}

// canonicalSemver gives a plain X.Y.Z the leading "v" golang.org/x/mod/semver
// requires. Template versions are stored without it
func canonicalSemver(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
