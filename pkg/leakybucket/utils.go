package leakybucket

import (
	"strconv"
	"time"
)

func Cap(value string, fallback uint32) uint32 {
	i, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return uint32(i)
}

func Interval(value string, fallback time.Duration) time.Duration {
	rps, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}

	interval := float64(time.Second) / rps

	return time.Duration(interval)
}
