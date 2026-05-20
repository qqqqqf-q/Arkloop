package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/jackc/pgx/v5"
)

// formatElapsed 返回两条消息之间的时间差的可读格式。
// 0 或负值返回空字符串；<1m 返回秒；<1h 返回分钟；<48h 返回小时；否则返回天。
func formatElapsed(prev, current time.Time) string {
	if prev.IsZero() || current.IsZero() {
		return ""
	}
	d := current.Sub(prev)
	if d <= 0 {
		return ""
	}
	totalSec := int(d.Seconds() + 0.5)
	if totalSec < 1 {
		return ""
	}
	if totalSec < 60 {
		return fmt.Sprintf("+%ds", totalSec)
	}
	minutes := int(d.Minutes() + 0.5)
	if minutes < 60 {
		return fmt.Sprintf("+%dm", minutes)
	}
	hours := int(d.Hours() + 0.5)
	if hours < 48 {
		return fmt.Sprintf("+%dh", hours)
	}
	days := int(d.Hours()/24 + 0.5)
	return fmt.Sprintf("+%dd", days)
}

// formatEnvelopeTime 将 time.Time 格式化为带 weekday 前缀的可读时间。
// 输出格式：Sat 13:31:05（weekday + HH:MM:SS）
func formatEnvelopeTime(t time.Time) string {
	if t.IsZero() {
		return "time?"
	}
	weekday := t.Format("Mon")
	return weekday + " " + t.Format("15:04:05")
}

// formatEnvelopeTimeShort 同 formatEnvelopeTime 但省略秒。
func formatEnvelopeTimeShort(t time.Time) string {
	if t.IsZero() {
		return "time?"
	}
	weekday := t.Format("Mon")
	return weekday + " " + t.Format("15:04")
}

// parseEnvelopeTime 解析 envelope 的 time 字段。
// 支持格式：RFC3339、RFC3339Nano、"YYYY-MM-DD HH:MM:SS [TZ]"。
func parseEnvelopeTime(raw string) time.Time {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return time.Time{}
	}
	if idx := strings.Index(cleaned, " ["); idx > 0 && strings.HasSuffix(cleaned, "]") {
		withoutTZ := cleaned[:idx]
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if t, err := time.Parse(layout, withoutTZ); err == nil {
				return t
			}
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, cleaned); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// formatInternalEnvelope 为平台内（Web/App）用户消息生成时间戳前缀。
// 格式：[Sun 14:30] 或 [+5m Sun 14:30]（有 elapsed 时）。
func formatInternalEnvelope(ts time.Time, elapsed string) string {
	timeStr := formatEnvelopeTimeShort(ts)
	if elapsed != "" {
		return fmt.Sprintf("[%s %s]", elapsed, timeStr)
	}
	return fmt.Sprintf("[%s]", timeStr)
}

// prependUserMessageTimestamp 为平台内 user 消息添加时间戳前缀。
// 已有 YAML envelope（channel 消息）的跳过。
// loc 为用户时区，nil 时使用 UTC。
func prependUserMessageTimestamp(parts []llm.ContentPart, createdAt time.Time, prevUserTime time.Time, loc *time.Location) []llm.ContentPart {
	if len(parts) == 0 || createdAt.IsZero() {
		return parts
	}
	firstText := -1
	for i, p := range parts {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			firstText = i
			break
		}
	}
	if firstText < 0 {
		return parts
	}
	if strings.HasPrefix(strings.TrimSpace(parts[firstText].Text), "---") {
		return parts
	}
	if loc == nil {
		loc = time.UTC
	}
	localTime := createdAt.In(loc)
	elapsed := formatElapsed(prevUserTime, createdAt)
	prefix := formatInternalEnvelope(localTime, elapsed)
	out := make([]llm.ContentPart, len(parts))
	copy(out, parts)
	p := out[firstText]
	p.Text = prefix + " " + p.Text
	out[firstText] = p
	return out
}

// resolveUserLocation 从数据库查询用户/账户时区设置，返回对应的 time.Location。
func resolveUserLocation(ctx context.Context, tx pgx.Tx, run data.Run) *time.Location {
	if tx == nil {
		return time.UTC
	}
	if run.CreatedByUserID != nil {
		var tz *string
		if err := tx.QueryRow(ctx, `SELECT timezone FROM users WHERE id = $1 LIMIT 1`, *run.CreatedByUserID).Scan(&tz); err == nil {
			if loc := parseTimeZone(tz); loc != nil {
				return loc
			}
		}
	}
	var tz *string
	if err := tx.QueryRow(ctx, `SELECT timezone FROM accounts WHERE id = $1 LIMIT 1`, run.AccountID).Scan(&tz); err == nil {
		if loc := parseTimeZone(tz); loc != nil {
			return loc
		}
	}
	return time.UTC
}

func parseTimeZone(value *string) *time.Location {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return nil
	}
	return loc
}
