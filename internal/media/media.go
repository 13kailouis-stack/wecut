// Package media holds what WeCut knows about a source file after probing it.
package media

// Info describes one probed file. Width and Height are display dimensions,
// with any rotation metadata already applied (FFmpeg auto-rotates on decode).
type Info struct {
	Path       string  `json:"-"` // absolute path, never shown to agents
	Rel        string  `json:"path"`
	Kind       string  `json:"kind"` // video, image, audio
	Duration   float64 `json:"duration,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	FPS        float64 `json:"fps,omitempty"`
	Rotation   int     `json:"rotation,omitempty"`
	HasAudio   bool    `json:"has_audio"`
	VideoCodec string  `json:"video_codec,omitempty"`
	AudioCodec string  `json:"audio_codec,omitempty"`
	SizeBytes  int64   `json:"size_bytes"`
}

// Set maps the src string used in the project to its probed file.
type Set map[string]Info
