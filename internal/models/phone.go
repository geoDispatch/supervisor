package models

import (
	"regexp"
	"strings"
)

// Phone numbers are personal data. Logs and free text (error messages,
// narratives, upstream bodies) go through these helpers; raw numbers appear
// only in structured "phone" fields and in the database.

var e164 = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// MaskPhone turns an E.164 number into "+<country code> <first digit>** *** <last 3>",
// e.g. "+212600000001" → "+212 6** *** 001", "+36719991001" → "+36 7** *** 001".
// The middle is a fixed pattern so the length of the number is not revealed.
// Anything that is not E.164 with at least 6 national digits returns "***".
func MaskPhone(p string) string {
	if !e164.MatchString(p) {
		return "***"
	}
	digits := p[1:]
	ccLen := countryCodeLen(digits)
	if len(digits)-ccLen < 6 {
		// Too short: first + last three would reveal most of the number.
		return "***"
	}
	cc, national := digits[:ccLen], digits[ccLen:]
	return "+" + cc + " " + national[:1] + "** *** " + national[len(national)-3:]
}

// countryCodeLen returns the length of the ITU country code that starts
// digits. E.164 codes are prefix-free: 1 and 7 are one digit, the list below
// is every two-digit code, and all remaining codes have three digits.
func countryCodeLen(digits string) int {
	switch digits[0] {
	case '1', '7':
		return 1
	}
	if len(digits) >= 2 && strings.Contains(twoDigitCountryCodes, "|"+digits[:2]+"|") {
		return 2
	}
	return 3
}

const twoDigitCountryCodes = "|20|27|30|31|32|33|34|36|39|40|41|43|44|45|46|47|48|49|" +
	"51|52|53|54|55|56|57|58|60|61|62|63|64|65|66|81|82|84|86|90|91|92|93|94|95|98|"

var digitRun = regexp.MustCompile(`\+?\d+`)

// RedactPhones masks everything in s that could be a phone number: every run
// of 8 or more consecutive digits, with or without a leading "+". E.164
// numbers become their MaskPhone form; bare runs become "***". Shorter
// numbers ("404", "5000ms", "batch 12") are left alone. Runs longer than 15
// digits are masked too rather than half-masked (they may embed a number).
// Numbers written with separators ("+212 600 000 001") are not detected.
func RedactPhones(s string) string {
	return digitRun.ReplaceAllStringFunc(s, func(run string) string {
		n := len(strings.TrimPrefix(run, "+"))
		if n < 8 {
			return run
		}
		return MaskPhone(run)
	})
}
