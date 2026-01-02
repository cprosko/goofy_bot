package main

import (
	// Standard Packages
	"os"

	// External Packages
	"gopkg.in/yaml.v3"
)

type Config struct {
	Token            string            `yaml:"bot_token"`
	ServerID         string            `yaml:"server_id"`
	VoiceChannelID   string            `yaml:"voice_channel_id"`
	MinInterval      int               `yaml:"min_interval_seconds"`
	MaxInterval      int               `yaml:"max_interval_seconds"`
	ExcludedSounds   []string          `yaml:"excluded_sounds"`
	UseDefaultSounds bool              `yaml:"use_default_sounds"`
	Responses        map[string]string `yaml:"responses"`
}

func parseConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	conf := &Config{}
	err = yaml.Unmarshal(data, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}
