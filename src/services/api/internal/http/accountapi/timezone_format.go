package accountapi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

type inboundTimeContext struct {
	TimeZone string
	Local    string
	UTC      string
}

func buildInboundTimeContext(ts time.Time, timeZone string) inboundTimeContext {
	utc := ts.UTC()
	loc := loadLocationOrUTC(timeZone)
	local := utc.In(loc)
	return inboundTimeContext{
		TimeZone: loc.String(),
		Local:    local.Format("2006-01-02 15:04:05") + " [" + formatUTCOffsetLabel(local) + "]",
		UTC:      utc.Format(time.RFC3339),
	}
}

func resolveInboundTimeZone(
	ctx context.Context,
	usersRepo *data.UserRepository,
	accountRepo *data.AccountRepository,
	accountID uuid.UUID,
	primaryUserID *uuid.UUID,
	fallbackUserID *uuid.UUID,
) string {
	for _, candidate := range []*uuid.UUID{primaryUserID, fallbackUserID} {
		if candidate == nil || *candidate == uuid.Nil || usersRepo == nil {
			continue
		}
		user, err := usersRepo.GetByID(ctx, *candidate)
		if err != nil || user == nil {
			continue
		}
		if normalized := normalizeIanaTimeZone(user.Timezone); normalized != "" {
			return normalized
		}
	}
	if accountRepo != nil && accountID != uuid.Nil {
		account, err := accountRepo.GetByID(ctx, accountID)
		if err == nil && account != nil {
			if normalized := normalizeIanaTimeZone(account.Timezone); normalized != "" {
				return normalized
			}
		}
	}
	return "UTC"
}

func normalizeIanaTimeZone(value *string) string {
	if value == nil {
		return ""
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return ""
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return ""
	}
	return loc.String()
}

func loadLocationOrUTC(timeZone string) *time.Location {
	cleaned := strings.TrimSpace(timeZone)
	if cleaned == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return time.UTC
	}
	return loc
}

func formatUTCOffsetLabel(t time.Time) string {
	_, offsetSeconds := t.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	if minutes == 0 {
		return "UTC" + sign + strconvInt(hours)
	}
	return "UTC" + sign + strconvInt(hours) + ":" + twoDigit(minutes)
}

func strconvInt(v int) string {
	return strconv.Itoa(v)
}

func twoDigit(v int) string {
	if v < 10 {
		return "0" + strconvInt(v)
	}
	return strconvInt(v)
}
