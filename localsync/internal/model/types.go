package model

import "time"

// SessionState holds the current playback state broadcast to connecting clients.
type SessionState struct {
	File     string    `json:"file"`
	Quality  string    `json:"quality"`
	Pos      float64   `json:"pos"`
	Paused   bool      `json:"paused"`
	Variants []Variant `json:"variants,omitempty"`
}

// Variant represents a pre-compressed video file in the .localsync/ folder.
type Variant struct {
	Name     string `json:"name"`     // display name (filename without extension)
	Filename string `json:"filename"` // actual filename in .localsync/
	Size     int64  `json:"size"`     // file size in bytes
}

// ClientInfo tracks a connected client's reported stats.
type ClientInfo struct {
	Name       string
	IP         string
	SpeedKbps  float64
	BufferSecs float64
	Pos        float64
	LastUpdate time.Time
}

// SyncMessage is the bidirectional sync protocol message.
type SyncMessage struct {
	Event  string   `json:"event"`
	State  *bool    `json:"state,omitempty"`
	Pos    float64  `json:"pos"`
	Source string   `json:"source"`
	Speed  *float64 `json:"speed,omitempty"`
}

// StatsMessage is sent periodically from client to server.
type StatsMessage struct {
	Event      string  `json:"event"`
	Source     string  `json:"source"`
	SpeedKbps  float64 `json:"speed_kbps"`
	BufferSecs float64 `json:"buffer_secs"`
	Pos        float64 `json:"pos"`
}
