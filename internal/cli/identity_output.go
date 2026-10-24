package cli

import (
	"strconv"
	"strings"
)

// humanIdentity preserves ordinary names while keeping controls and ambiguous
// spellings on one visible line. JSON uses its own exact-value encoding.
func humanIdentity(value string) string {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\"\\") || strings.IndexFunc(value, func(r rune) bool { return !strconv.IsPrint(r) }) >= 0 {
		return strconv.Quote(value)
	}
	return value
}
