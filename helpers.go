package main

import "strings"

func valueOrDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
