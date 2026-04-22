package coerce

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Bool coerces a value to bool. Accepts bool, "true"/"false" (case-insensitive).
func Bool(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("cannot coerce %T(%v) to bool", v, v)
}

// Int coerces a value to int. Accepts int, float64 (whole numbers), and numeric strings.
func Int(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case float64:
		if val != math.Trunc(val) {
			return 0, fmt.Errorf("cannot coerce float64(%v) to int: not a whole number", val)
		}
		return int(val), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return 0, fmt.Errorf("cannot coerce string(%q) to int: %w", val, err)
		}
		return n, nil
	}
	return 0, fmt.Errorf("cannot coerce %T(%v) to int", v, v)
}

// Float coerces a value to float64. Accepts float64, int, and numeric strings.
func Float(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce string(%q) to float64: %w", val, err)
		}
		return f, nil
	}
	return 0, fmt.Errorf("cannot coerce %T(%v) to float64", v, v)
}
