package main

import (
	// Standard Packages
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"

	// External Packages

	"github.com/bwmarrin/discordgo"
)

type Sound struct {
	ID        string `json:"sound_id"`
	Name      string `json:"name"`
	EmojiID   string `json:"emoji_id"`
	EmojiName string `json:"emoji_name"`
	GuildID   string `json:"guild_id"`
	Available bool   `json:"available"`
}

type SoundManager struct {
	sync.RWMutex
	AvailableIDs []string
}

func (sm *SoundManager) GetRandomID() string {
	sm.RLock()
	defer sm.RUnlock()
	if len(sm.AvailableIDs) == 0 {
		return ""
	}
	return sm.AvailableIDs[rand.IntN(len(sm.AvailableIDs))]
}

func (sm *SoundManager) UpdateIDs(newIDs []string) {
	sm.Lock()
	sm.AvailableIDs = newIDs
	sm.Unlock()
}

func fetchDefaultSounds(
	session *discordgo.Session,
	guildID string,
) ([]Sound, error) {
	endpoint := discordgo.EndpointGuild(guildID) + "/soundboard-default-sounds"
	return fetchSounds(session, endpoint)
}

func fetchGuildSounds(
	session *discordgo.Session,
	guildID string,
) ([]Sound, error) {
	endpoint := discordgo.EndpointGuild(guildID) + "/soundboard-sounds"
	return fetchSounds(session, endpoint)
}

func fetchSounds(
	session *discordgo.Session,
	endpoint string,
) ([]Sound, error) {
	// Perform GET request
	body, err := session.Request("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("Request failed: %w", err)
	}
	// API returns a JSON object where sounds are under the 'items' key
	var response struct {
		Items []Sound `json:"items"`
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	return response.Items, nil
}

func availableSounds(
	allSounds []Sound,
	conf *Config,
) []string {
	excludedSet := make(map[string]struct{})
	for _, id := range conf.ExcludedSounds {
		excludedSet[id] = struct{}{}
	}
	var pool []string
	// Only add sounds to sound pool NOT in excluded list
	for _, sound := range allSounds {
		if _, exists := excludedSet[sound.ID]; !exists {
			pool = append(pool, sound.ID)
		}
	}
	return pool
}

