package usageapi_test

import (
	"context"
	"fmt"

	"github.com/McMelonTV/check-usage/usageapi"
)

// Example lists cached usage for every saved account. Every type in the
// results is part of this package, so callers outside the module can use them.
func Example() {
	service := usageapi.New(usageapi.Config{})
	results, err := service.Usage(context.Background(), "", false)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, result := range results {
		for _, metric := range result.Metrics {
			if metric.Kind == usageapi.Percentage && metric.Used != nil {
				fmt.Printf("%s %s: %.0f%%\n", result.Account.Name, metric.Label, *metric.Used)
			}
		}
	}
}
