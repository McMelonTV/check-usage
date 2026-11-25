package nativecli

import (
	"context"
	"errors"
)

// Interactive agy needs a pseudo-terminal, which this probe supports only on Unix.
func fetchAgyPlan(context.Context, string, string, []string) (string, error) {
	return "", errors.New("agy plan probe is not supported on Windows")
}
