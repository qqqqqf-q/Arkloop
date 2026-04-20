package schedulekind

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	Interval = "interval"
	Daily    = "daily"
	Monthly  = "monthly"
	Weekdays = "weekdays"
	Weekly   = "weekly"
	At       = "at"
	Cron     = "cron"
)

func SupportsDeleteAfterRun(kind string) bool {
	return kind == At
}

// Validate 校验调度参数组合是否合法。
func Validate(kind string, intervalMin *int, dailyTime string, monthlyDay *int, monthlyTime string, weeklyDay *int, fireAt *time.Time, cronExpr string, tz string) error {
	switch kind {
	case Interval:
		if intervalMin == nil || *intervalMin < 1 {
			return fmt.Errorf("interval_min must be >= 1 for interval schedule")
		}
	case Daily:
		if _, _, err := parseHHMM(dailyTime); err != nil {
			return fmt.Errorf("invalid daily_time: %w", err)
		}
	case Monthly:
		if monthlyDay == nil || *monthlyDay < 1 || *monthlyDay > 28 {
			return fmt.Errorf("monthly_day must be between 1 and 28")
		}
		if _, _, err := parseHHMM(monthlyTime); err != nil {
			return fmt.Errorf("invalid monthly_time: %w", err)
		}
	case Weekdays:
		if _, _, err := parseHHMM(dailyTime); err != nil {
			return fmt.Errorf("invalid daily_time: %w", err)
		}
	case Weekly:
		if weeklyDay == nil || *weeklyDay < 0 || *weeklyDay > 6 {
			return fmt.Errorf("weekly_day must be between 0 and 6")
		}
		if _, _, err := parseHHMM(dailyTime); err != nil {
			return fmt.Errorf("invalid daily_time: %w", err)
		}
	case At:
		if fireAt == nil || fireAt.IsZero() {
			return fmt.Errorf("fire_at is required for 'at' schedule kind")
		}
	case Cron:
		if strings.TrimSpace(cronExpr) == "" {
			return fmt.Errorf("cron_expr is required for 'cron' schedule kind")
		}
	default:
		return fmt.Errorf("unknown schedule kind %q", kind)
	}

	_, err := CalcNextFire(
		kind,
		derefInt(intervalMin),
		dailyTime,
		derefIntOr(monthlyDay, 1),
		monthlyTime,
		derefIntOr(weeklyDay, 0),
		derefTime(fireAt),
		cronExpr,
		tz,
		time.Now().UTC(),
	)
	return err
}

// CalcNextFire 根据调度类型计算下一次触发时间。
func CalcNextFire(kind string, intervalMin int, dailyTime string, monthlyDay int, monthlyTime string, weeklyDay int, fireAt time.Time, cronExpr string, tz string, now time.Time) (time.Time, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil && kind != At {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	if loc == nil {
		loc = time.UTC
	}

	switch kind {
	case Interval:
		return now.Add(time.Duration(intervalMin) * time.Minute), nil

	case Daily:
		h, m, err := parseHHMM(dailyTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid daily_time: %w", err)
		}
		local := now.In(loc)
		t := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t.UTC(), nil

	case Monthly:
		h, m, err := parseHHMM(monthlyTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid monthly_time: %w", err)
		}
		local := now.In(loc)
		day := clampDay(monthlyDay, local.Month(), local.Year())
		t := time.Date(local.Year(), local.Month(), day, h, m, 0, 0, loc)
		if !t.After(now) {
			// 推到下月
			nextMonth := local.Month() + 1
			nextYear := local.Year()
			if nextMonth > 12 {
				nextMonth = 1
				nextYear++
			}
			day = clampDay(monthlyDay, nextMonth, nextYear)
			t = time.Date(nextYear, nextMonth, day, h, m, 0, 0, loc)
		}
		return t.UTC(), nil

	case Weekdays:
		h, m, err := parseHHMM(dailyTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid daily_time: %w", err)
		}
		local := now.In(loc)
		t := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
		if !t.After(now) || isWeekend(local.Weekday()) {
			t = nextWeekday(t)
		}
		return t.UTC(), nil

	case Weekly:
		h, m, err := parseHHMM(dailyTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid daily_time: %w", err)
		}
		local := now.In(loc)
		// 计算目标星期几与今天的偏移
		diff := int(time.Weekday(weeklyDay)) - int(local.Weekday())
		t := time.Date(local.Year(), local.Month(), local.Day()+diff, h, m, 0, 0, loc)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t.UTC(), nil

	case At:
		if fireAt.IsZero() {
			return time.Time{}, fmt.Errorf("fire_at is required for 'at' schedule kind")
		}
		return fireAt.UTC(), nil

	case Cron:
		if cronExpr == "" {
			return time.Time{}, fmt.Errorf("cron_expr is required for 'cron' schedule kind")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := parser.Parse(cronExpr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
		}
		return sched.Next(now.In(loc)).UTC(), nil

	default:
		return time.Time{}, fmt.Errorf("unknown schedule kind %q", kind)
	}
}

// parseHHMM 解析 "HH:MM" 格式的时间字符串。
func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h, m, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefIntOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// clampDay 将天数限制在指定月份的有效范围内。
func clampDay(day int, month time.Month, year int) int {
	// 下月1日减一天 = 本月最后一天
	maxDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > maxDay {
		return maxDay
	}
	return day
}

// isWeekend 判断是否为周末。
func isWeekend(wd time.Weekday) bool {
	return wd == time.Saturday || wd == time.Sunday
}

// nextWeekday 返回下一个工作日（跳过周末）。
func nextWeekday(t time.Time) time.Time {
	for {
		t = t.AddDate(0, 0, 1)
		if !isWeekend(t.Weekday()) {
			return t
		}
	}
}
