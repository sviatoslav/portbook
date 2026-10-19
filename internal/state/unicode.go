package state

import (
	"errors"
	"strconv"
	"unicode/utf8"
)

// ValidateJSONUnicode prevents encoding/json from repairing invalid UTF-8 or
// unpaired surrogate escapes and silently changing persisted identities.
// Call after JSON syntax validation; escaped backslashes remain literal text.
func ValidateJSONUnicode(data []byte) error {
	invalid := errors.New("text must be valid Unicode without unpaired surrogate escapes")
	if !utf8.Valid(data) {
		return invalid
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return invalid
		}
		code, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return invalid
		}
		i += 4
		switch {
		case code >= 0xd800 && code <= 0xdbff:
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return invalid
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return invalid
			}
			i += 6
		case code >= 0xdc00 && code <= 0xdfff:
			return invalid
		}
	}
	return nil
}
