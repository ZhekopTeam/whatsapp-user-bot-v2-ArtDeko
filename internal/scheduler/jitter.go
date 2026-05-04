package scheduler

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

func buildUniqueMinuteOffsets(rng *rand.Rand, count int, minMinutes int, maxMinutes int) ([]int, error) {
	if count <= 0 {
		return nil, nil
	}
	if minMinutes > maxMinutes {
		minMinutes, maxMinutes = maxMinutes, minMinutes
	}
	available := maxMinutes - minMinutes + 1
	if available < count {
		return nil, fmt.Errorf("not enough unique minute values in range %d-%d", minMinutes, maxMinutes)
	}

	values := make([]int, 0, available)
	for minute := minMinutes; minute <= maxMinutes; minute++ {
		values = append(values, minute)
	}
	rng.Shuffle(len(values), func(i int, j int) {
		values[i], values[j] = values[j], values[i]
	})

	selected := append([]int(nil), values[:count]...)
	sort.Ints(selected)
	return selected, nil
}

func selectScheduledDates(start time.Time, end time.Time, countDays int) []time.Time {
	start = normalizeDay(start)
	end = normalizeDay(end)
	if end.Before(start) {
		return nil
	}

	totalDays := int(end.Sub(start).Hours()/24) + 1
	if countDays <= 0 || countDays >= totalDays {
		dates := make([]time.Time, 0, totalDays)
		for index := 0; index < totalDays; index++ {
			dates = append(dates, start.AddDate(0, 0, index))
		}
		return dates
	}

	if countDays == 1 {
		return []time.Time{start}
	}

	selected := make([]time.Time, 0, countDays)
	usedOffsets := make(map[int]struct{}, countDays)
	for index := 0; index < countDays; index++ {
		ratio := float64(index) / float64(countDays-1)
		offset := int(math.Round(ratio * float64(totalDays-1)))
		if _, exists := usedOffsets[offset]; exists {
			for step := 0; step < totalDays; step++ {
				if _, exists := usedOffsets[step]; !exists {
					offset = step
					break
				}
			}
		}
		usedOffsets[offset] = struct{}{}
		selected = append(selected, start.AddDate(0, 0, offset))
	}
	return selected
}

func normalizeDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}
