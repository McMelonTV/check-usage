package nativecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

func fetchCodex(ctx context.Context, executable, dir string, config Config) (Result, error) {
	cmd := exec.CommandContext(ctx, executable, "app-server")
	cmd.Dir, cmd.Env = dir, environment(config.Env)
	configureProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return Result{}, errors.New("cannot start Codex app-server")
	}
	defer func() { _ = input.Close(); killProcess(cmd); _ = cmd.Wait() }()
	reader := bufio.NewScanner(io.LimitReader(output, 4<<20))
	reader.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(input)
	call := func(id int, method string, params any, target any) error {
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return errors.New("cannot write native Codex request")
		}
		for reader.Scan() {
			var message struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(reader.Bytes(), &message) != nil {
				return errors.New("invalid native Codex RPC response")
			}
			if message.Method != "" && message.ID != nil {
				return errors.New("Codex requested an interactive action; sign in on the server")
			}
			if message.ID == nil || *message.ID != id {
				continue
			}
			if len(message.Error) > 0 && string(message.Error) != "null" {
				return fmt.Errorf("native Codex %s failed; check the server's CLI login", method)
			}
			if target != nil && json.Unmarshal(message.Result, target) != nil {
				return errors.New("invalid native Codex result")
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Codex app-server closed before returning usage")
	}
	if err := call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "check_usage", "version": "0.1.0"}}, nil); err != nil {
		return Result{}, err
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return Result{}, err
	}
	var identity struct {
		Account *struct {
			Type  string `json:"type"`
			Email string `json:"email"`
			Plan  string `json:"planType"`
		} `json:"account"`
		UserID string `json:"userId"`
	}
	if err := call(2, "account/read", map[string]bool{"refreshToken": false}, &identity); err != nil {
		return Result{}, err
	}
	if identity.Account == nil || identity.Account.Type != "chatgpt" {
		return Result{}, errors.New("Codex subscription login required on the server")
	}
	var limits codexLimits
	if err := call(3, "account/rateLimits/read", nil, &limits); err != nil {
		return Result{}, err
	}
	result, err := mapCodexLimits(limits)
	result.Email, result.AccountID = identity.Account.Email, identity.UserID
	if result.Usage.Plan == "" {
		result.Usage.Plan = identity.Account.Plan
	}
	return result, err
}

type codexWindow struct {
	Used    *float64 `json:"usedPercent"`
	Minutes *int     `json:"windowDurationMins"`
	ResetAt *int64   `json:"resetsAt"`
}
type codexBucket struct {
	Plan      string       `json:"planType"`
	Name      string       `json:"limitName"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}
type codexLimits struct {
	Limits  codexBucket            `json:"rateLimits"`
	Buckets map[string]codexBucket `json:"rateLimitsByLimitId"`
	Resets  *struct {
		Available int `json:"availableCount"`
		Credits   *[]struct {
			Status    string `json:"status"`
			Title     string `json:"title"`
			GrantedAt int64  `json:"grantedAt"`
			ExpiresAt *int64 `json:"expiresAt"`
		} `json:"credits"`
	} `json:"rateLimitResetCredits"`
}

func mapCodexLimits(data codexLimits) (Result, error) {
	result := Result{Usage: providers.Usage{Plan: data.Limits.Plan}}
	appendBucket := func(bucket codexBucket, scope string) {
		for i, window := range []*codexWindow{bucket.Primary, bucket.Secondary} {
			if window == nil {
				continue
			}
			slot, label := providers.SessionSlot, "SESSION"
			if i == 1 {
				slot, label = providers.WeeklySlot, "WEEKLY"
			}
			if window.Minutes != nil {
				switch {
				case *window.Minutes >= 28*24*60:
					slot, label = providers.MonthlySlot, "MONTHLY"
				case *window.Minutes >= 7*24*60:
					slot, label = providers.WeeklySlot, "WEEKLY"
				}
			}
			result.Usage.Metrics = append(result.Usage.Metrics, providers.Metric{Kind: providers.Percentage, Slot: slot, Label: strings.TrimSpace(scope + " " + label), Scope: scope, Used: window.Used, ResetAt: window.ResetAt})
		}
	}
	if len(data.Buckets) == 0 {
		appendBucket(data.Limits, "")
	} else {
		keys := make([]string, 0, len(data.Buckets))
		for key := range data.Buckets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			bucket := data.Buckets[key]
			scope := bucket.Name
			if key == "codex" {
				scope = ""
			} else if scope == "" {
				scope = key
			}
			appendBucket(bucket, scope)
		}
	}
	if len(result.Usage.Metrics) == 0 {
		return Result{}, errors.New("Codex reported no quota windows")
	}
	if data.Resets != nil && data.Resets.Credits != nil {
		credits := &codexapi.ResetCreditsPayload{AvailableCount: data.Resets.Available, TotalEarnedUnknown: true, Credits: []codexapi.ResetCreditDetail{}}
		for _, credit := range *data.Resets.Credits {
			detail := codexapi.ResetCreditDetail{Status: credit.Status, Title: credit.Title, GrantedAt: time.Unix(credit.GrantedAt, 0).UTC().Format(time.RFC3339)}
			if credit.ExpiresAt != nil {
				detail.ExpiresAt = time.Unix(*credit.ExpiresAt, 0).UTC().Format(time.RFC3339)
			}
			credits.Credits = append(credits.Credits, detail)
		}
		result.ResetCredits = credits
	}
	return result, validateUsage(result.Usage)
}
