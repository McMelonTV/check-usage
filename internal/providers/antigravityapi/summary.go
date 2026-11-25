package antigravityapi

import (
	"errors"
	"strings"
	"time"
)

var ErrLimitsUnavailable = errors.New("Antigravity CLI reported no measured quota limits")

type Summary struct {
	Response *Summary `json:"response,omitempty"`
	Summary  *Summary `json:"summary,omitempty"`
	Groups   []Group  `json:"groups,omitempty"`
}
type Group struct {
	DisplayName string   `json:"displayName"`
	Name        string   `json:"name"`
	Buckets     []Bucket `json:"buckets"`
}
type Bucket struct {
	ID                string   `json:"bucketId"`
	LegacyID          string   `json:"id"`
	DisplayName       string   `json:"displayName"`
	Name              string   `json:"name"`
	Window            string   `json:"window"`
	Disabled          bool     `json:"disabled"`
	RemainingFraction *float64 `json:"remainingFraction"`
	Remaining         *struct {
		RemainingFraction *float64 `json:"remainingFraction"`
		Case              string   `json:"case"`
		Value             *float64 `json:"value"`
	} `json:"remaining"`
	ResetTime string `json:"resetTime"`
}

type Quota struct {
	Group   string
	Window  string // session or weekly; never inferred from the reset countdown
	Used    *float64
	ResetAt *int64
}

func (summary Summary) Quotas() ([]Quota, error) {
	if summary.Response != nil {
		return summary.Response.Quotas()
	}
	if summary.Summary != nil {
		return summary.Summary.Quotas()
	}
	var quotas []Quota
	known := false
	for _, group := range summary.Groups {
		name := strings.TrimSpace(group.DisplayName)
		if name == "" {
			name = strings.TrimSpace(group.Name)
		}
		if name == "" {
			continue
		}
		for _, bucket := range group.Buckets {
			if bucket.Disabled {
				continue
			}
			window := quotaWindow(bucket.Window + " " + bucket.ID + " " + bucket.LegacyID + " " + bucket.DisplayName + " " + bucket.Name)
			if window == "" {
				continue
			}
			quota := Quota{Group: name, Window: window}
			fraction := bucket.RemainingFraction
			if fraction == nil && bucket.Remaining != nil {
				fraction = bucket.Remaining.RemainingFraction
				if fraction == nil && bucket.Remaining.Case == "remainingFraction" {
					fraction = bucket.Remaining.Value
				}
			}
			// Omitted fractions mean unknown, not zero or full.
			if fraction != nil && *fraction >= 0 && *fraction <= 1 {
				used := (1 - *fraction) * 100
				quota.Used, known = &used, true
			}
			if reset, err := time.Parse(time.RFC3339Nano, bucket.ResetTime); err == nil {
				value := reset.Unix()
				quota.ResetAt = &value
			}
			quotas = append(quotas, quota)
		}
	}
	if !known {
		return nil, ErrLimitsUnavailable
	}
	return quotas, nil
}

func quotaWindow(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })
	// Split digit-bearing tokens without treating unrelated words as cadences.
	for i, word := range words {
		switch word {
		case "weekly", "week", "7d", "10080m":
			return "weekly"
		case "session", "5h", "300m":
			return "session"
		case "5", "five":
			if i+1 < len(words) && (words[i+1] == "hour" || words[i+1] == "hours") {
				return "session"
			}
		}
	}
	return ""
}
