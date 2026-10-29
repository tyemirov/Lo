package adapter

import (
	"fmt"
	"time"
)

type fileMetadata struct {
	owner                       uint32
	accessed, modified, changed int64
}

func fileTimeRepresentable(stamp time.Time, unixNS int64) bool {
	return time.Unix(0, unixNS).Equal(stamp)
}

func fileUnixNanoseconds(seconds, nanoseconds int64) (int64, error) {
	if nanoseconds < 0 || nanoseconds >= 1_000_000_000 {
		return 0, fmt.Errorf("invalid timestamp nanoseconds")
	}
	stamp := time.Unix(seconds, nanoseconds)
	nanosecondTime := stamp.UnixNano()
	if !fileTimeRepresentable(stamp, nanosecondTime) {
		return 0, fmt.Errorf("timestamp exceeds Unix nanosecond range")
	}
	return nanosecondTime, nil
}
