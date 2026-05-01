package vacation

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusApproved   Status = "approved"
	StatusOverridden Status = "overridden"
	StatusCancelled  Status = "cancelled"
)

type Vacation struct {
	ID        uuid.UUID
	UserID    string
	UserName  string
	StartDate time.Time
	EndDate   time.Time
	Note      string
	Status    Status
	CreatedAt time.Time
	CreatedBy string
}

type Settings struct {
	MinPresent    int
	TeamSize      int
	WeekendCounts bool
}
