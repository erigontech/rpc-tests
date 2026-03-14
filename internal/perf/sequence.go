package perf

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TestSequenceItem represents a single test in the sequence.
type TestSequenceItem struct {
	QPS      int
	Duration int
}

// TestSequence is a list of test sequence items.
type TestSequence []TestSequenceItem

// ParseTestSequence parses the test sequence string "QPS:Duration,..." into structured items.
func ParseTestSequence(sequence string) (TestSequence, error) {
	var items TestSequence

	for part := range strings.SplitSeq(sequence, ",") {
		qpsDur := strings.Split(part, ":")
		if len(qpsDur) != 2 {
			return nil, fmt.Errorf("invalid test sequence format: %s", part)
		}

		qps, err := strconv.Atoi(qpsDur[0])
		if err != nil {
			return nil, fmt.Errorf("invalid QPS value: %s", qpsDur[0])
		}

		duration, err := strconv.Atoi(qpsDur[1])
		if err != nil {
			return nil, fmt.Errorf("invalid duration value: %s", qpsDur[1])
		}

		items = append(items, TestSequenceItem{
			QPS:      qps,
			Duration: duration,
		})
	}

	return items, nil
}

// ResultFormat holds formatting widths for console output alignment.
type ResultFormat struct {
	MaxRepetitionDigits int
	MaxQpsDigits        int
	MaxDurationDigits   int
}

// CountDigits returns the number of decimal digits in n.
func CountDigits(n int) int {
	if n == 0 {
		return 1
	}
	digits := 0
	for n != 0 {
		n /= 10
		digits++
	}
	return digits
}

// MaxQpsAndDurationDigits computes the max digit widths across a sequence.
func MaxQpsAndDurationDigits(sequence TestSequence) (maxQpsDigits, maxDurationDigits int) {
	for _, item := range sequence {
		qpsDigits := CountDigits(item.QPS)
		if qpsDigits > maxQpsDigits {
			maxQpsDigits = qpsDigits
		}
		durationDigits := CountDigits(item.Duration)
		if durationDigits > maxDurationDigits {
			maxDurationDigits = durationDigits
		}
	}
	return
}

// FormatDuration formats a duration string with appropriate units.
func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// ParseLatency parses a latency string and returns it in a consistent format.
func ParseLatency(latency string) string {
	latency = strings.ReplaceAll(latency, "µs", "us")
	return strings.TrimSpace(latency)
}
