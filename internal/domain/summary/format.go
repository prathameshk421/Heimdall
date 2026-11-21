package summary

import (
	"fmt"
	"strings"
	"time"
)

// maxMoney is far above any real spend. The range check also refuses not-a-number and infinity.
const maxMoney = 1e12

func validMoney(v float64) bool { return v >= 0 && v < maxMoney }

// money writes a dollar amount. A non-zero amount under one cent is "<$0.01", so a real cost never reads as $0.00.
func money(v float64) string {
	if v > 0 && v < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

// tokens writes a token count in a short form: 950, 412.3k, 1.25M.
func tokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return trim(fmt.Sprintf("%.1f", float64(n)/1000)) + "k"
	}
	return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000)) + "M"
}

// wall writes a duration in whole seconds: 45s, 4m 12s, 1h 02m.
func wall(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm", s/3600, s%3600/60)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// trim drops a fraction that is all zeros: "800.0" is "800", "1.50" is "1.5".
func trim(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}
