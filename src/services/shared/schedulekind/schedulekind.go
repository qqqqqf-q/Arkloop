package schedulekind

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	Interval = "interval"
	Daily    = "daily"
	Monthly  = "monthly"
	Weekdays = "weekdays"
	Weekly   = "weekly"
)

// CalcNextFire 根据调度类型计算下一次触发时间。
func CalcNextFire(kind string, intervalMin int, dailyTime string, monthlyDay int, monthlyTime string, weeklyDay int, tz string, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
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
