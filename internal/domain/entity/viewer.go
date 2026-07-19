package entity

import "time"

// Viewer represents a friend with a read-only dashboard account
// (viewers table, D-27). Viewers log in with email + password (bcrypt) and
// may only browse active sources. They are a separate entity from
// Subscriber: web access control and podcast delivery control have
// independent lifecycles (D-27 (1)).
type Viewer struct {
	ID            int64
	Name          string
	Email         string
	PasswordHash  string // bcrypt。ハンドラ層(DTO)には決して載せない
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeactivatedAt *time.Time // nil = アクティブ
}

// IsActive reports whether the viewer may currently log in and browse.
func (v *Viewer) IsActive() bool {
	return v.DeactivatedAt == nil
}
