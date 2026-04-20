package schedulekind

import (
	"testing"
	"time"
)

func TestCalcNextFire(t *testing.T) {
	utc := time.UTC
	shanghai, _ := time.LoadLocation("Asia/Shanghai")

	tests := []struct {
		name        string
		kind        string
		intervalMin int
		dailyTime   string
		monthlyDay  int
		monthlyTime string
		weeklyDay   int
		tz          string
		now         time.Time
		want        time.Time
		wantErr     bool
	}{
		{
			name:        "interval basic",
			kind:        Interval,
			intervalMin: 30,
			tz:          "UTC",
			now:         time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			want:        time.Date(2026, 4, 15, 10, 30, 0, 0, utc),
		},
		{
			name:      "daily today not passed",
			kind:      Daily,
			dailyTime: "18:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			want:      time.Date(2026, 4, 15, 18, 0, 0, 0, utc),
		},
		{
			name:      "daily today already passed",
			kind:      Daily,
			dailyTime: "08:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			want:      time.Date(2026, 4, 16, 8, 0, 0, 0, utc),
		},
		{
			name:        "monthly this month not passed",
			kind:        Monthly,
			monthlyDay:  20,
			monthlyTime: "09:00",
			tz:          "UTC",
			now:         time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			want:        time.Date(2026, 4, 20, 9, 0, 0, 0, utc),
		},
		{
			name:        "monthly this month already passed",
			kind:        Monthly,
			monthlyDay:  10,
			monthlyTime: "09:00",
			tz:          "UTC",
			now:         time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			want:        time.Date(2026, 5, 10, 9, 0, 0, 0, utc),
		},
		{
			name:        "monthly day=31 in february clamps to 28",
			kind:        Monthly,
			monthlyDay:  31,
			monthlyTime: "12:00",
			tz:          "UTC",
			now:         time.Date(2026, 1, 31, 13, 0, 0, 0, utc),
			want:        time.Date(2026, 2, 28, 12, 0, 0, 0, utc),
		},
		{
			name:        "monthly day=31 in april clamps to 30",
			kind:        Monthly,
			monthlyDay:  31,
			monthlyTime: "12:00",
			tz:          "UTC",
			now:         time.Date(2026, 3, 31, 13, 0, 0, 0, utc),
			want:        time.Date(2026, 4, 30, 12, 0, 0, 0, utc),
		},
		{
			name:      "daily with Asia/Shanghai timezone",
			kind:      Daily,
			dailyTime: "09:00",
			tz:        "Asia/Shanghai",
			// UTC 00:00 = Shanghai 08:00, 所以 09:00 Shanghai 还未过
			now:  time.Date(2026, 4, 15, 0, 0, 0, 0, utc),
			want: time.Date(2026, 4, 15, 9, 0, 0, 0, shanghai).UTC(),
		},
		{
			name:    "unknown kind returns error",
			kind:    "yearly",
			tz:      "UTC",
			now:     time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			wantErr: true,
		},
		{
			name:      "invalid daily_time format",
			kind:      Daily,
			dailyTime: "9pm",
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			wantErr:   true,
		},
		{
			name:    "invalid timezone",
			kind:    Interval,
			tz:      "Mars/Olympus",
			now:     time.Date(2026, 4, 15, 10, 0, 0, 0, utc),
			wantErr: true,
		},
		{
			name:      "weekdays same day not passed",
			kind:      Weekdays,
			dailyTime: "18:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 15, 18, 0, 0, 0, utc),
		},
		{
			name:      "weekdays same day already passed",
			kind:      Weekdays,
			dailyTime: "08:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 16, 8, 0, 0, 0, utc),  // Thu
		},
		{
			name:      "weekdays on friday after time",
			kind:      Weekdays,
			dailyTime: "08:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 17, 10, 0, 0, 0, utc), // Fri
			want:      time.Date(2026, 4, 20, 8, 0, 0, 0, utc),  // Mon
		},
		{
			name:      "weekdays on saturday",
			kind:      Weekdays,
			dailyTime: "09:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 18, 8, 0, 0, 0, utc), // Sat
			want:      time.Date(2026, 4, 20, 9, 0, 0, 0, utc), // Mon
		},
		{
			name:      "weekdays on sunday",
			kind:      Weekdays,
			dailyTime: "09:00",
			tz:        "UTC",
			now:       time.Date(2026, 4, 19, 8, 0, 0, 0, utc), // Sun
			want:      time.Date(2026, 4, 20, 9, 0, 0, 0, utc), // Mon
		},
		{
			name:      "weekly same day not passed",
			kind:      Weekly,
			dailyTime: "18:00",
			weeklyDay: 3, // Wed
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 15, 18, 0, 0, 0, utc),
		},
		{
			name:      "weekly same day already passed",
			kind:      Weekly,
			dailyTime: "08:00",
			weeklyDay: 3, // Wed
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 22, 8, 0, 0, 0, utc),  // next Wed
		},
		{
			name:      "weekly earlier weekday",
			kind:      Weekly,
			dailyTime: "09:00",
			weeklyDay: 1, // Mon
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 20, 9, 0, 0, 0, utc),  // next Mon
		},
		{
			name:      "weekly later weekday",
			kind:      Weekly,
			dailyTime: "09:00",
			weeklyDay: 5, // Fri
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 17, 9, 0, 0, 0, utc),  // Fri
		},
		{
			name:      "weekly sunday",
			kind:      Weekly,
			dailyTime: "09:00",
			weeklyDay: 0, // Sun
			tz:        "UTC",
			now:       time.Date(2026, 4, 15, 10, 0, 0, 0, utc), // Wed
			want:      time.Date(2026, 4, 19, 9, 0, 0, 0, utc),  // Sun
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalcNextFire(tt.kind, tt.intervalMin, tt.dailyTime, tt.monthlyDay, tt.monthlyTime, tt.weeklyDay, time.Time{}, "", tt.tz, tt.now)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
