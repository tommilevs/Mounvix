package plugins

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a strict five-field UTC cron expression.
type Schedule struct {
	minute, hour, day, month, weekday [64]bool
	dayWildcard, weekdayWildcard      bool
}

// ParseSchedule accepts exactly five UTC cron fields; fields support only * or one integer.
func ParseSchedule(expr string) (Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Schedule{}, errors.New("schedule must contain exactly five cron fields")
	}
	bounds := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	sets := []*[64]bool{}
	var result Schedule
	sets = append(sets, &result.minute, &result.hour, &result.day, &result.month, &result.weekday)
	for i, field := range fields {
		if field == "*" {
			for n := bounds[i][0]; n <= bounds[i][1]; n++ {
				sets[i][n] = true
			}
			if i == 2 {
				result.dayWildcard = true
			}
			if i == 4 {
				result.weekdayWildcard = true
			}
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || strconv.Itoa(value) != field || value < bounds[i][0] || value > bounds[i][1] {
			return Schedule{}, fmt.Errorf("invalid cron field %q", field)
		}
		sets[i][value] = true
	}
	return result, nil
}

// Matches reports whether a UTC instant is selected by the schedule.
func (s Schedule) Matches(at time.Time) bool {
	at = at.UTC()
	if at.Second() != 0 || at.Nanosecond() != 0 || !s.minute[at.Minute()] || !s.hour[at.Hour()] || !s.month[int(at.Month())] {
		return false
	}
	dom := s.day[at.Day()]
	dow := s.weekday[int(at.Weekday())]
	if !s.dayWildcard && !s.weekdayWildcard {
		return dom || dow
	}
	return dom && dow
}
