package shared

import (
	// Standard Packages ---------------------------------------------------------

	// Allows unmarshalling of soundboard data from Discord's API
	"encoding/json"
	// For formatting errors
	"fmt"
	// For selecting random sounds
	"math/rand/v2"
	// Provides Mutexes for safe concurrency
	"sync"

	// External Packages ---------------------------------------------------------

	// Provides interface with Discord's API for bots
	"github.com/bwmarrin/discordgo"
)

// Sound includes all information about a Soundboard sound on Discord
type Sound struct {
	ID        string `json:"sound_id"`
	Name      string `json:"name"`
	EmojiID   string `json:"emoji_id"`
	EmojiName string `json:"emoji_name"`
	GuildID   string `json:"guild_id"`
	Available bool   `json:"available"`
}

// SoundManager enables safe access and writing of available soundboard sounds.
type SoundManager struct {
	sync.RWMutex
	AvailableIDs []string
}

// GetRandomID returns a randomly selected soundboard sound ID.
// Uses a SoundManager struct as input to provide the list of available sounds.
func (sm *SoundManager) GetRandomID() string {
	sm.RLock()
	defer sm.RUnlock()
	if len(sm.AvailableIDs) == 0 {
		return ""
	}
	return sm.AvailableIDs[rand.IntN(len(sm.AvailableIDs))]
}

// UpdateIDs safely updates the available sound IDs in a SoundManager
func (sm *SoundManager) UpdateIDs(newIDs []string) {
	sm.Lock()
	sm.AvailableIDs = newIDs
	sm.Unlock()
}

// AvailableSounds returns a filtered list of sound info excluding some sounds.
// The excluded sounds are specified by the input Config.ExcludedSounds.
func AvailableSounds(
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

// FetchDefaultSounds returns the sound info for Discord's default sounds.
// It also returns an error if API requests or data unmarshalling fails.
func FetchDefaultSounds(
	session *discordgo.Session,
	guildID string,
) ([]Sound, error) {
	endpoint := discordgo.EndpointGuild(guildID) + "/soundboard-default-sounds"
	return fetchSounds(session, endpoint)
}

// FetchGuildSounds returns the sound info for custom sounds on a Guild/Server.
// It also returns an error if API requests or data unmarshalling fails.
func FetchGuildSounds(
	session *discordgo.Session,
	guildID string,
) ([]Sound, error) {
	endpoint := discordgo.EndpointGuild(guildID) + "/soundboard-sounds"
	return fetchSounds(session, endpoint)
}

// fetchSounds attempts to unmarshal a Discord API endpoint into Sound info.
// It also returns an error if the API request or data unmarshalling fails.
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
