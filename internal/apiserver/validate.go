package apiserver

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Request-supplied ids are checked before they reach the store, the
// background job table or a Riot request, so junk can't be stored, queued
// for crawling, or spend the Riot key's quota.
var (
	// Riot PUUIDs are 78 characters of base64url; allow some slack.
	puuidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	// Match ids are "<PLATFORM>_<number>", e.g. "NA1_5123456789".
	matchIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{2,6}_[0-9]{1,20}$`)
)

func validPUUID(s string) bool   { return len(s) <= 100 && puuidPattern.MatchString(s) }
func validMatchID(s string) bool { return len(s) <= 32 && matchIDPattern.MatchString(s) }

// validRiotID checks a Riot ID's shape: game names are up to 16 characters
// and tag lines up to 5, in any script, with no control characters or
// path separators.
func validRiotID(gameName, tagLine string) bool {
	return validRiotIDPart(gameName, 16) && validRiotIDPart(tagLine, 5)
}

func validRiotIDPart(s string, maxRunes int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	if n := utf8.RuneCountInString(s); n > maxRunes {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '/' || r == '\\' || r == '#' || r == '?' {
			return false
		}
	}
	return true
}
