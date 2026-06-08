// SPDX-License-Identifier: Apache-2.0

package guardrails

import (
	"regexp"
	"strings"
	"unicode"
)

type regexRule struct {
	typ     string
	re      *regexp.Regexp
	replace func(string) string
}

func defaultRegexRules() []regexRule {
	return []regexRule{
		{
			typ: "EMAIL_ADDRESS",
			re:  regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`),
		},
		{
			typ: "US_SSN",
			re:  regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
		{
			typ: "PHONE_NUMBER",
			re:  regexp.MustCompile(`\b(?:\+?1[-.\s]?)?(?:\(?\d{3}\)?[-.\s]?)\d{3}[-.\s]?\d{4}\b`),
		},
		{
			typ: "CREDIT_CARD",
			re:  regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
			replace: func(s string) string {
				digits := digitsOnly(s)
				if len(digits) < 13 || len(digits) > 19 || !validLuhn(digits) {
					return s
				}
				return "[REDACTED_CREDIT_CARD]"
			},
		},
		{
			typ: "API_KEY",
			re:  regexp.MustCompile(`\b(?:sk|pk|rk|af)_[A-Za-z0-9][A-Za-z0-9_-]{16,}\b`),
		},
		{
			typ: "AWS_ACCESS_KEY_ID",
			re:  regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
		},
	}
}

func redactWithRegex(text string) (string, []Finding, bool) {
	out := text
	findings := []Finding{}
	for _, rule := range defaultRegexRules() {
		matches := rule.re.FindAllStringIndex(out, -1)
		if len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			findings = append(findings, Finding{
				Type:   rule.typ,
				Start:  m[0],
				End:    m[1],
				Source: ProviderRegex,
			})
		}
		if rule.replace != nil {
			out = rule.re.ReplaceAllStringFunc(out, rule.replace)
			continue
		}
		out = rule.re.ReplaceAllString(out, "[REDACTED_"+rule.typ+"]")
	}
	return out, findings, out != text
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validLuhn(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := int(digits[i] - '0')
		if n < 0 || n > 9 {
			return false
		}
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0
}
