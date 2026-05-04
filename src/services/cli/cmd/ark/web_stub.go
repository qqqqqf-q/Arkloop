//go:build !desktop

package main

import (
	"context"
	"fmt"
)

func cmdWeb(_ context.Context, _ []string) error {
	return fmt.Errorf("ark web requires a desktop-enabled build")
}
