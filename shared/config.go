/*
shared implements auxiliary data structures which are used by the bot logic.

These functions and structs are those which are used by the bot, but which do
not depend on the bot's internal state outside of its Config parameters.
*/
package shared

import (
	// Standard Packages ---------------------------------------------------------

	// Allows file reading and environment variable access
	"os"
	// Provides time.Duration for specifying real times
	"time"

	// External Packages ---------------------------------------------------------

	// For unmarshalling the config.yaml into a Config object
	"gopkg.in/yaml.v3"
)

// Config stores all parameters of the bot which are static at runtime.
type Config struct {
	// The bot token used to access the Discord API and connect to a server
	Token                string              `yaml:"bot_token"`
	// The ID number of the Discord server to join
	ServerID             string              `yaml:"server_id"`
	// The ID number of the Discord voice channel to connect to and speak on
	VoiceChannelID       string              `yaml:"voice_channel_id"`
	// The minimum number of seconds between the bot playing random sounds
	MinSoundInterval     int                 `yaml:"min_sound_interval_seconds"`
	// The maximum number of seconds between the bot playing random sounds
	MaxSoundInterval     int                 `yaml:"max_sound_interval_seconds"`
	// The probability [0, 1] of a played sound being played multiply in rapid
	// succession
	RapidFireProbability float32             `yaml:"rapid_fire_probability"`
	// The minimum milliseconds between repeats in a rapidly-played sound
	RapidFireMinInterval int                 `yaml:"rapid_fire_min_interval_milliseconds"`
	// The maximum milliseconds between repeats in a rapidly-played sound
	RapidFireMaxInterval int                 `yaml:"rapid_fire_max_interval_milliseconds"`
	// The minimum repeats of a rapidly-played sound
	RapidFireCountMin    int                 `yaml:"rapid_fire_count_min"`
	// The maximum repeats of a rapidly-played sound
	RapidFireCountMax    int                 `yaml:"rapid_fire_count_max"`
	// Soundboard sound ID's which should never be played by the bot
	ExcludedSounds       []string            `yaml:"excluded_sounds"`
	// Whether to also play sounds from Discord's default soundboard
	UseDefaultSounds     bool                `yaml:"use_default_sounds"`
	// Messages to write to the channel's chat in response to commands.
	// For now, the only command which the bot responds to is "!refresh"
	CommandResponses     map[string]string   `yaml:"command_responses"`
	VoiceModel           string              `yaml:"voice_model"`
	// The *.onnx filename of the Piper voice model to use for synthesizing voice.
	// This file should exist in the same directory as the main package's main.go
	ResponseProbability  float32             `yaml:"response_probability"`
	// Minimum seconds between playing a voice clip due to a user leaving or
	// entering the channel.
	Cooldown             time.Duration       `yaml:"response_cooldown"`
	// Minimum seconds between playing a random voice clip
	MinVoiceInterval     int                 `yaml:"min_voice_interval_seconds"`
	// Maximum seconds between playing a random voice clip
	MaxVoiceInterval     int                 `yaml:"max_voice_interval_seconds"`
	// Nested map of text to synthesize to voice and play.
	// Should contain responses for keys "left" (when user leaves), "joined"
	// (when user joins), and "random" (for randomly played voice clips)
	Responses            map[string][]string `yaml:"responses"`
}

// addDefaultCommandResponses inserts default command responses if unconfigured.
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

// ParseConfig reads a .yaml file and returns it unmarshalled into a Config.
// Also returns an error if file reading or unmarshalling has an error.
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
