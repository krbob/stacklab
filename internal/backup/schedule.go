package backup

import (
	"fmt"
	"time"
)

type SchedulePreview struct {
	NextOccurrences []time.Time `json:"next_occurrences"`
	Timezone        string      `json:"timezone"`
}

func validateSchedule(schedule Schedule) error {
	switch schedule.Frequency {
	case "", "manual":
		return nil
	case "interval":
		if schedule.IntervalHours < 1 || schedule.IntervalHours > 168 {
			return fmt.Errorf("%w: interval_hours must be 1 to 168", ErrInvalid)
		}
		return nil
	case "daily", "weekly":
	default:
		return fmt.Errorf("%w: schedule frequency must be manual, interval, daily, or weekly", ErrInvalid)
	}
	if _, err := time.Parse("15:04", schedule.Time); err != nil {
		return fmt.Errorf("%w: schedule time must be HH:MM", ErrInvalid)
	}
	if schedule.Weekday < 0 || schedule.Weekday > 6 {
		return fmt.Errorf("%w: schedule weekday must be 0 to 6", ErrInvalid)
	}
	if schedule.Timezone != "" {
		if _, err := time.LoadLocation(schedule.Timezone); err != nil {
			return fmt.Errorf("%w: unknown schedule timezone", ErrInvalid)
		}
	}
	return nil
}
func scheduleLocation(schedule Schedule) *time.Location {
	if schedule.Frequency == "interval" {
		return time.UTC
	}
	if schedule.Timezone != "" {
		if location, err := time.LoadLocation(schedule.Timezone); err == nil {
			return location
		}
	}
	return time.Local
}
func calendarOccurrence(schedule Schedule, date time.Time) (time.Time, bool) {
	parsed, err := time.Parse("15:04", schedule.Time)
	if err != nil {
		return time.Time{}, false
	}
	location := scheduleLocation(schedule)
	candidate := time.Date(date.Year(), date.Month(), date.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	// Skip nonexistent DST wall times instead of silently moving a job by an hour.
	if candidate.Hour() != parsed.Hour() || candidate.Minute() != parsed.Minute() || candidate.Day() != date.Day() {
		return time.Time{}, false
	}
	if schedule.Frequency == "weekly" && int(candidate.Weekday()) != schedule.Weekday {
		return time.Time{}, false
	}
	return candidate, true
}

// latestDue uses calendar dates for wall-clock schedules, and a fixed UTC epoch
// for intervals. A missed occurrence is attempted once, without catch-up storms.
func latestDue(schedule Schedule, now time.Time) (time.Time, bool) {
	if validateSchedule(schedule) != nil || schedule.Frequency == "manual" || schedule.Frequency == "" {
		return time.Time{}, false
	}
	if schedule.Frequency == "interval" {
		seconds := int64(schedule.IntervalHours) * 3600
		return time.Unix(now.Unix()/seconds*seconds, 0).UTC(), true
	}
	local := now.In(scheduleLocation(schedule))
	for days := 0; days < 15; days++ {
		date := local.AddDate(0, 0, -days)
		if candidate, ok := calendarOccurrence(schedule, date); ok && !candidate.After(now) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// PreviewSchedule returns future occurrences using exactly the scheduler's
// calendar rules. The bound avoids unbounded work from API input.
func PreviewSchedule(schedule Schedule, from time.Time, count int) ([]time.Time, error) {
	if err := validateSchedule(schedule); err != nil {
		return nil, err
	}
	if count < 1 || count > 10 {
		return nil, fmt.Errorf("%w: schedule preview count must be 1 to 10", ErrInvalid)
	}
	result := []time.Time{}
	if schedule.Frequency == "manual" || schedule.Frequency == "" {
		return result, nil
	}
	if schedule.Frequency == "interval" {
		due, _ := latestDue(schedule, from)
		interval := time.Duration(schedule.IntervalHours) * time.Hour
		for i := 1; i <= count; i++ {
			result = append(result, due.Add(time.Duration(i)*interval))
		}
		return result, nil
	}
	local := from.In(scheduleLocation(schedule))
	for days := 0; len(result) < count && days < 100; days++ {
		date := local.AddDate(0, 0, days)
		if candidate, ok := calendarOccurrence(schedule, date); ok && candidate.After(from) {
			result = append(result, candidate)
		}
	}
	return result, nil
}
func (s *Service) SchedulePreview(schedule Schedule, counts ...int) (SchedulePreview, error) {
	count := 5
	if len(counts) > 0 && counts[0] != 0 {
		count = counts[0]
	}
	occurrences, err := PreviewSchedule(schedule, s.now(), count)
	zone := scheduleLocation(schedule).String()
	if zone == "Local" {
		// RFC3339 occurrences carry their exact offset even if the host timezone is
		// an unnamed /etc/localtime rather than a resolvable IANA identifier.
		zone = "UTC"
		for index := range occurrences {
			occurrences[index] = occurrences[index].UTC()
		}
	}
	return SchedulePreview{NextOccurrences: occurrences, Timezone: zone}, err
}

// Future watermarks after a backwards clock correction must not suppress new
// work until the old clock catches up. A started occurrence replaces them once.
const clockSkewTolerance = 5 * time.Minute

func occurrencePending(last *time.Time, due, now time.Time) bool {
	return last == nil || last.After(now.Add(clockSkewTolerance)) || last.Before(due)
}

// The scheduler coalesces missed runs to the latest occurrence, but monitoring
// must retain the first unsatisfied deadline. Advancing the calendar cannot
// renew grace for a copy that has not been replaced.
func firstOutstandingDue(assignment Assignment, captured *time.Time, now time.Time, grace time.Duration) (time.Time, bool) {
	var from time.Time
	if captured != nil && !captured.After(now.Add(clockSkewTolerance)) {
		from = *captured
	}
	if activated := assignment.ScheduleActivatedAt; activated != nil && !activated.After(now.Add(clockSkewTolerance)) {
		// A capture at the occurrence satisfies it; an activation at the
		// occurrence includes it, matching the existing activation contract.
		activation := activated.Add(-time.Nanosecond)
		if from.IsZero() || activation.After(from) {
			from = activation
		}
	}
	if from.IsZero() {
		// Legacy state without activation or capture cannot prove a fresh grace
		// period. Treat an already expired occurrence as outstanding rather than
		// inventing a new deadline on each tick or service restart.
		return latestDue(assignment.Schedule, now.Add(-grace).Add(-time.Nanosecond))
	}
	next, err := PreviewSchedule(assignment.Schedule, from, 1)
	if err != nil || len(next) == 0 || next[0].After(now) {
		return time.Time{}, false
	}
	return next[0], true
}
