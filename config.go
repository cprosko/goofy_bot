package main

import (
	// Standard Packages
	"log"
	"os"

	// External Packages
	"gopkg.in/yaml.v3"
)

type Config struct {
	Token          string   `yaml:"bot_token"`
	ServerID       string   `yaml:"server_id"`
	VoiceChannelID string   `yaml:"voice_channel_id"`
	MinInterval    int      `yaml:"min_interval_seconds"`
	MaxInterval    int      `yaml:"max_interval_seconds"`
	ExcludedSounds []string `yaml:"excluded_sounds"`
}

func parseConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Unable to read file at path %s:\n%s\n", path, err)
	}
	var conf Config
	err = yaml.Unmarshal(data, &conf)
	if err != nil {
		log.Fatalf("Unable to parse YAML in file %s: \n%s\n", path, err)
	}
	return conf
}
