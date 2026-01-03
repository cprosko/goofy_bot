package main

import (
	// Standard Packages
	"os"

	// External Packages
	"gopkg.in/yaml.v3"
)

type Config struct {
	Token                string            `yaml:"bot_token"`
	ServerID             string            `yaml:"server_id"`
	VoiceChannelID       string            `yaml:"voice_channel_id"`
	MinInterval          int               `yaml:"min_interval_seconds"`
	MaxInterval          int               `yaml:"max_interval_seconds"`
	RapidFireProbability float32           `yaml:"rapid_fire_probability"`
	RapidFireMinInterval int               `yaml:"rapid_fire_min_interval_milliseconds"`
	RapidFireMaxInterval int               `yaml:"rapid_fire_max_interval_milliseconds"`
	RapidFireCountMin    int               `yaml:"rapid_fire_count_min"`
	RapidFireCountMax    int               `yaml:"rapid_fire_count_max"`
	ExcludedSounds       []string          `yaml:"excluded_sounds"`
	UseDefaultSounds     bool              `yaml:"use_default_sounds"`
	CommandResponses     map[string]string `yaml:"command_responses"`
	ResponseProbability  float32           `yaml:"response_probability"`
	Responses            []string          `yaml:"responses"`
}

func (c *Config) addDefaultCommandResponses() {
	if c.CommandResponses == nil {
		c.CommandResponses = make(map[string]string)
	}

	defaultResponses := map[string]string{
		"refresh": "Refreshed soundboard!",
	}

	for cmd, resp := range defaultResponses {
		_, exists := c.CommandResponses[cmd]
		if !exists {
			c.CommandResponses[cmd] = resp
		}
	}
}

func ParseConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	conf := &Config{}
	err = yaml.Unmarshal(data, conf)
	if err != nil {
		return nil, err
	}

	token := os.Getenv("DISCORD_BOT_TOKEN")
	conf.Token = token

	conf.addDefaultCommandResponses()
	return conf, nil
}
