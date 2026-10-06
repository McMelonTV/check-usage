package usage

import (
	"strings"
)

func ValueOrDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func FirstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
