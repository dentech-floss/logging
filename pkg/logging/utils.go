package logging

import (
	"regexp"
	"strings"
)

// MaskSSN finds and masks any SSN in the format YYYYMMDDXXXX in the input string
func MaskSSN(input string) string {
	// Regular expression to find Swedish SSN in the format YYYYMMDDXXXX
	re := regexp.MustCompile(`\b\d{8}\d{4}\b`)

	// Mask function to replace SSN with a masked version
	masked := re.ReplaceAllStringFunc(input, func(ssn string) string {
		// Mask everything except the last 4 digits
		return strings.Repeat("X", 8) + ssn[8:]
	})

	return masked
}

// MaskPhone finds and masks phone numbers, keeping the first 3 and last 3 digits visible, and filters out date-like patterns
func MaskPhone(input string) string {

	// Updated regular expression to find phone numbers with spaces or dashes allowed
	re := regexp.MustCompile(`(\+46|0)[\d\s-]{9,12}`)

	// Mask function to replace phone number with a masked version
	masked := re.ReplaceAllStringFunc(input, func(phone string) string {

		// Remove non-digit characters for masking
		plainPhone := strings.ReplaceAll(strings.ReplaceAll(phone, " ", ""), "-", "")

		// if it's probably a date rather than a phone number, skip masking
		if isDateLike(plainPhone) {
			return phone
		}

		// Ensure phone has at least 7 characters after the first 3
		if len(plainPhone) <= 8 {
			return phone
		}

		// Mask the middle part while preserving spaces and dashes in the original format
		midLen := len(plainPhone) - 6
		maskedPhone := plainPhone[:3] + strings.Repeat("X", midLen) + plainPhone[len(plainPhone)-3:]

		// Reinsert spaces and dashes from the original phone number
		index := 0
		finalPhone := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return r
			}
			res := rune(maskedPhone[index])
			index++
			return res
		}, phone)

		return finalPhone
	})

	return masked
}

// MaskEmail finds and masks email addresses
func MaskEmail(input string) string {
	// Regular expression to find email addresses
	re := regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)

	// Mask function to replace email with a masked version
	masked := re.ReplaceAllStringFunc(input, func(email string) string {
		parts := strings.Split(email, "@")
		localPart := parts[0]
		domain := parts[1]

		// If the local part is longer than 2 characters, mask the middle
		if len(localPart) > 2 {
			// Keep the first and last character, mask the middle part
			maskedLocal := localPart[:1] + strings.Repeat("X", len(localPart)-2) + localPart[len(localPart)-1:]
			return maskedLocal + "@" + domain
		}
		// If the local part is too short, mask everything
		return strings.Repeat("X", len(localPart)) + "@" + domain
	})

	return masked
}

func MaskString(
	input string,
	toMask string,
) string {
	return strings.ReplaceAll(input, toMask, strings.Repeat("X", len(toMask)))
}

// isDateLike checks if a given string looks like a date in formats like YYYYMMDD or YYYY-MM-DD
func isDateLike(s string) bool {
	// Match both YYYY-MM-DD and YYYYMMDD (strictly checking for valid dates)
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}Z)?$|^\d{8}$`)
	return re.MatchString(s)
}
