package claudeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResetProgram is the server-side name of Claude's limit-reset program. Its
// status block is returned by the usage endpoint when asked with
// "?cedar_ember=1", and resets are claimed (not implemented here) through
// POST /api/organizations/{org}/reset_rate_limits with {"program": ResetProgram}.
const ResetProgram = "cedar_ember"

// ResetStatus is the "cedar_ember" block of the usage payload: the limit
// resets ("reset credits") the account holds. Field names follow the API.
type ResetStatus struct {
	Eligible         bool         `json:"eligible"`
	IneligibleReason string       `json:"ineligible_reason,omitempty"`
	AtLimit          bool         `json:"at_limit,omitempty"`
	Grants           []ResetGrant `json:"grants,omitempty"`
	NextGrantID      string       `json:"next_grant_id,omitempty"`
	WeeklyResetsAt   string       `json:"weekly_resets_at,omitempty"`
	CooldownUntil    string       `json:"cooldown_until,omitempty"`
}

// ResetGrant is one batch of resets, e.g. "3 resets until the end of the month".
type ResetGrant struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	ResetsTotal int      `json:"resets_total"`
	ResetsLeft  int      `json:"resets_left"`
	StartsAt    string   `json:"starts_at,omitempty"`
	EndsAt      string   `json:"ends_at,omitempty"`
	Clears      []string `json:"clears,omitempty"`
	Paused      bool     `json:"paused,omitempty"`
	UsableNow   bool     `json:"usable_now,omitempty"`
	// Type is "session", "weekly" or "full" when the API states it.
	Type string `json:"type,omitempty"`
}

// rawResetStatus mirrors the wire format with nullable fields and grants kept
// raw, so one malformed grant is dropped instead of failing the whole block.
type rawResetStatus struct {
	Eligible         *bool             `json:"eligible"`
	IneligibleReason *string           `json:"ineligible_reason"`
	AtLimit          *bool             `json:"at_limit"`
	Grants           []json.RawMessage `json:"grants"`
	NextGrantID      *string           `json:"next_grant_id"`
	WeeklyResetsAt   *string           `json:"weekly_resets_at"`
	CooldownUntil    *string           `json:"cooldown_until"`
}

type rawResetGrant struct {
	ID          string   `json:"id"`
	Label       *string  `json:"label"`
	ResetsTotal *int     `json:"resets_total"`
	ResetsLeft  *int     `json:"resets_left"`
	StartsAt    *string  `json:"starts_at"`
	EndsAt      *string  `json:"ends_at"`
	Clears      []string `json:"clears"`
	Paused      *bool    `json:"paused"`
	UsableNow   *bool    `json:"usable_now"`
	Type        *string  `json:"type"`
}

// ResetStatus parses the limit-reset block. It returns nil without an error
// when the API sent none. Absence does not establish the account's balance.
func (payload *UsagePayload) ResetStatus() (*ResetStatus, error) {
	if payload == nil {
		return nil, nil
	}
	raw := bytes.TrimSpace(payload.Resets)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var wire rawResetStatus
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode Claude reset status: %w", err)
	}
	if wire.Eligible == nil {
		return nil, fmt.Errorf("decode Claude reset status: missing eligible")
	}
	status := &ResetStatus{
		Eligible: *wire.Eligible, IneligibleReason: deref(wire.IneligibleReason), AtLimit: derefBool(wire.AtLimit),
		WeeklyResetsAt: deref(wire.WeeklyResetsAt), CooldownUntil: deref(wire.CooldownUntil),
	}
	for _, rawGrant := range wire.Grants {
		if grant, ok := parseResetGrant(rawGrant); ok {
			status.Grants = append(status.Grants, grant)
		}
	}
	// Like Claude Code, only trust a next grant the list actually contains.
	if next := deref(wire.NextGrantID); next != "" {
		for _, grant := range status.Grants {
			if grant.ID == next {
				status.NextGrantID = next
				break
			}
		}
	}
	return status, nil
}

func parseResetGrant(raw json.RawMessage) (ResetGrant, bool) {
	var wire rawResetGrant
	if err := json.Unmarshal(raw, &wire); err != nil || strings.TrimSpace(wire.ID) == "" || wire.ResetsLeft == nil || *wire.ResetsLeft < 0 {
		return ResetGrant{}, false
	}
	grant := ResetGrant{
		ID: wire.ID, Label: deref(wire.Label), ResetsLeft: *wire.ResetsLeft,
		StartsAt: deref(wire.StartsAt), EndsAt: deref(wire.EndsAt), Clears: wire.Clears,
		Paused: derefBool(wire.Paused), UsableNow: derefBool(wire.UsableNow), Type: deref(wire.Type),
	}
	if wire.ResetsTotal != nil && *wire.ResetsTotal >= 0 {
		grant.ResetsTotal = *wire.ResetsTotal
	}
	grant.ResetsTotal = max(grant.ResetsTotal, grant.ResetsLeft)
	return grant, true
}

// Holding reports whether the grant still has resets the account can use:
// some are left, it is not paused, and it has not ended. This is the same
// test Claude Code applies before advertising an unused reset.
func (grant ResetGrant) Holding(now time.Time) bool {
	if grant.ResetsLeft <= 0 || grant.Paused {
		return false
	}
	endsAt, ok := grant.EndTime()
	return !ok || endsAt.After(now)
}

// EndTime parses EndsAt; a grant without one does not expire.
func (grant ResetGrant) EndTime() (time.Time, bool) {
	return parseResetTime(grant.EndsAt)
}

// AvailableResets counts the resets held across all usable grants.
func (status *ResetStatus) AvailableResets(now time.Time) int {
	if status == nil || !status.Eligible {
		return 0
	}
	count := 0
	for _, grant := range status.Grants {
		if grant.Holding(now) {
			count += grant.ResetsLeft
		}
	}
	return count
}

func parseResetTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func derefBool(value *bool) bool {
	return value != nil && *value
}
