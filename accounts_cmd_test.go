package main

import (
	"net/http"
	"testing"
)

func TestShouldUpsertMatchedAccount_NameMismatchSkipsUpsert(t *testing.T) {
	existing := storedAccount{Name: "primary", PlanType: strPtr("plus")}
	candidate := storedAccount{Name: "secondary", PlanType: strPtr("plus")}

	called := false
	upsert, reason, _, _ := shouldUpsertMatchedAccount(existing, candidate, "secondary", nil, func(acc storedAccount, _ *http.Client) (storedAccount, string, bool) {
		called = true
		return acc, "", false
	})

	if upsert {
		t.Fatalf("expected upsert to be skipped when --name differs")
	}
	if reason != "a different --name was provided" {
		t.Fatalf("unexpected reason: %q", reason)
	}
	if called {
		t.Fatalf("did not expect plan re-check for name mismatch")
	}
}

func TestShouldUpsertMatchedAccount_PlanMismatchAfterRecheckSkipsUpsert(t *testing.T) {
	existing := storedAccount{Name: "existing", PlanType: strPtr("plus")}
	candidate := storedAccount{Name: "candidate", PlanType: strPtr("pro")}

	upsert, reason, _, _ := shouldUpsertMatchedAccount(existing, candidate, "", nil, func(acc storedAccount, _ *http.Client) (storedAccount, string, bool) {
		if acc.Name == "existing" {
			return acc, "plus", true
		}
		return acc, "pro", true
	})

	if upsert {
		t.Fatalf("expected upsert to be skipped when plan differs after re-check")
	}
	if reason != "a different plan was detected after re-check" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}
