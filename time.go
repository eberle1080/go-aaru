package aaru

import "time"

// filetimeEpochDiff is the number of seconds between 1601-01-01 and 1970-01-01.
const filetimeEpochDiff = 11644473600

// FiletimeToTime converts a Windows FILETIME (100 ns ticks since 1601-01-01
// UTC), as stored in AaruFormat headers, to a time.Time in UTC.
func FiletimeToTime(ft int64) time.Time {
	secs := ft/10_000_000 - filetimeEpochDiff
	nsec := (ft % 10_000_000) * 100
	return time.Unix(secs, nsec).UTC()
}

// TimeToFiletime converts t to a Windows FILETIME (100 ns ticks since
// 1601-01-01 UTC).
func TimeToFiletime(t time.Time) int64 {
	return (t.Unix()+filetimeEpochDiff)*10_000_000 + int64(t.Nanosecond())/100
}

// nowFiletime returns the current time as a FILETIME.
func nowFiletime() int64 { return TimeToFiletime(time.Now()) }
