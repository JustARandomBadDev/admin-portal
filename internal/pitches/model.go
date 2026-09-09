package pitches

import "time"

// Pitch is an admin-side pitch that can be linked to WiFi tickets.
type Pitch struct {
	ID        string
	Code      string
	Label     string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PitchCreateInput contains the fields required to create a pitch.
type PitchCreateInput struct {
	Code  string
	Label string
}

// PitchUpdateInput contains editable pitch fields.
type PitchUpdateInput struct {
	ID       string
	Code     string
	Label    string
	IsActive bool
}
