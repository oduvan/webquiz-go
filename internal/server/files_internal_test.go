package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The modification time of a listed file is rounded like Python's
// datetime.fromtimestamp(os.stat(path).st_mtime): the seconds become a
// float, then microseconds are rounded half to even. Expected values from
// Python 3.13 (shown in UTC).
func TestFromTimestampLikePython(t *testing.T) {
	cases := []struct {
		sec, nsec int64
		wantUTC   string
	}{
		{1731234567, 123456789, "2024-11-10T10:29:27.123457"},
		{1731234567, 999999999, "2024-11-10T10:29:28"},
		{1731234567, 500, "2024-11-10T10:29:27"},
		{1731234567, 1500, "2024-11-10T10:29:27.000001"},
		{1731234567, 2500, "2024-11-10T10:29:27.000002"},
		{1731234567, 0, "2024-11-10T10:29:27"},
		{1700000000, 999999500, "2023-11-14T22:13:21"},
		{1700000000, 123456500, "2023-11-14T22:13:20.123456"},
	}
	for _, c := range cases {
		got := fromTimestamp(time.Unix(c.sec, c.nsec)).UTC()
		text := got.Format("2006-01-02T15:04:05")
		if micro := got.Nanosecond() / 1000; micro != 0 {
			text += fmt.Sprintf(".%06d", micro)
		}
		if text != c.wantUTC || got.Nanosecond()%1000 != 0 {
			t.Errorf("%d.%09d: %s (%d ns), want %s", c.sec, c.nsec, text, got.Nanosecond(), c.wantUTC)
		}
	}
}

func TestIsSafeFilename(t *testing.T) {
	for name, want := range map[string]bool{
		"0001.log": true, "Математика 5 клас.yaml": true, "con": true, "a b.csv": true, ".hidden": true,
		strings.Repeat("я", 255): true, strings.Repeat("я", 256): false,
		"": false, ".": false, "..": false, "a..b": false, "../x": false, "a/b": false, `a\b`: false, "a\x00b": false,
	} {
		if got := isSafeFilename(name); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}
