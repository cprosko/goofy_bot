package main

import (
	// Standard Packages

	// External Packages
	"gopkg.in/yaml.v3"
)

type Config struct {
	VoiceChannelID string   `yaml:"voice_channel_id"`
	MinInterval    int      `yaml:"min_interval_seconds"`
	MaxInterval    int      `yaml:"max_interval_seconds"`
	ExcludedSounds []string `yaml:"excluded_sounds"`
}

func parseConfig(path string) Config {
	// TODO: not implemented
	return Config{}
}
