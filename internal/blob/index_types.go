package blob

import (
	"time"
)

type TimeFilter struct {
	Before *time.Time
	After  *time.Time
}

// bulkUpdateResult contains statistics about a bulk update operation
type bulkUpdateResult struct {
	Added   int
	Updated int
	Deleted int
}
