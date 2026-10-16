// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package speaker stores speaker profiles, append-only consent and private voice samples.
package speaker

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("speaker: not found")

const (
	Recognition    = "recognition"
	Storage        = "storage"
	SyntheticVoice = "synthetic_voice"
	Terms          = "terms"
)

type Profile struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Languages   []string  `json:"languages"`
	Style       string    `json:"style"`
	Notes       string    `json:"notes,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Consent struct {
	ID                            string    `json:"id"`
	ProfileID                     string    `json:"profileId"`
	Permission                    string    `json:"permission"`
	TextVersion                   string    `json:"textVersion"`
	Granted                       bool      `json:"granted"`
	At                            time.Time `json:"at"`
	By                            string    `json:"by"`
	OrganiserHasWrittenPermission bool      `json:"organiserHasWrittenPermission"`
	RetentionDays                 int       `json:"retentionDays,omitempty"`
}

type Checks struct {
	Analysed        bool     `json:"analysed"`
	DurationSeconds float64  `json:"durationSeconds"`
	PeakDBFS        *float64 `json:"peakDbfs,omitempty"`
	RMSDBFS         *float64 `json:"rmsDbfs,omitempty"`
	NoiseDBFS       *float64 `json:"noiseDbfs,omitempty"`
	Clipping        bool     `json:"clipping"`
	Message         string   `json:"message,omitempty"`
}

type Sample struct {
	ID              string    `json:"id"`
	ProfileID       string    `json:"profileId"`
	FileName        string    `json:"fileName"`
	FilePath        string    `json:"filePath"`
	Transcript      string    `json:"transcript,omitempty"`
	DurationSeconds float64   `json:"durationSeconds"`
	Checks          Checks    `json:"checks"`
	UploadedAt      time.Time `json:"uploadedAt"`
	DeleteAfter     time.Time `json:"deleteAfter"`
}

type Store interface {
	SaveProfile(context.Context, Profile) (Profile, error)
	Profiles(context.Context) ([]Profile, error)
	Profile(context.Context, string) (Profile, error)
	AddConsent(context.Context, Consent) (Consent, error)
	Consents(context.Context, string) ([]Consent, error)
	AddSample(context.Context, Sample) (Sample, error)
	Samples(context.Context, string) ([]Sample, error)
	DeleteSample(context.Context, string, string) (Sample, error)
	DeleteExpired(context.Context, time.Time) ([]Sample, error)
	Close() error
}
