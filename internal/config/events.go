package config

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

// WithCreated adds events created by the admin UI to the seed catalog.
func WithCreated(cfg Config, path string) (Config, error) {
	extra, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg.Rooms = append(cfg.Rooms, extra.Rooms...)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

// AddCreated atomically persists only UI-created events, leaving rooms.yaml
// untouched. The caller serializes additions for this single-server catalog.
func AddCreated(path string, rc Room) error {
	cfg, err := Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, r := range cfg.Rooms {
		if r.ID == rc.ID {
			return fmt.Errorf("duplicate event %q", rc.ID)
		}
	}
	cfg.Rooms = append(cfg.Rooms, rc)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if _, err = Parse(data); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".events-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
