package bot

import (
	// Internal packages ---------------------------------------------------------

	// Provides data structures and logic for configuration and soundboard sounds
	"goofybot/shared"

	// Standard packages ---------------------------------------------------------

	// Allows creating formatted error objects
	"fmt"
	// For printing errors and messages to the log
	"log"
	// For probabilistically playing sounds
	"math/rand/v2"
	// Provides functionality for waiting/sleeping
	"time"

	// External packages ---------------------------------------------------------

	// Provides interface with Discord's API for bots
	"github.com/bwmarrin/discordgo"
)

// Tolerance to consider a probability identically 'zero'
const tolerance float32 = 0.0001

// RefreshSounds updates the bot's knowledge of the Discord soundboard
func (b *Bot) RefreshSounds() {
	customSounds, err := shared.FetchGuildSounds(b.Session, b.Config.ServerID)
	if err != nil {
		log.Printf("Error fetching custom sounds: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.CustomSounds = shared.AvailableSounds(customSounds, b.Config)

	var finalPool []string
	if b.Config.UseDefaultSounds {
		// Only fetch defaults if we haven't already
		if len(b.DefaultSounds) == 0 {
			defaultSounds, err := shared.FetchDefaultSounds(b.Session, b.Config.ServerID)
			if err != nil {
				log.Printf("Error fetching default sounds: %v", err)
			} else {
				b.DefaultSounds = shared.AvailableSounds(defaultSounds, b.Config)
			}
		}
		finalPool = append(b.CustomSounds, b.DefaultSounds...)
	} else {
		finalPool = b.CustomSounds
	}

	b.SoundManager.UpdateIDs(finalPool)
	log.Printf("Sounds refreshed. Total pool size: %d", len(finalPool))
}

// StartSoundLoop begins infinite loop randomly playing soundboard sounds.
// To be used as a goroutine.
func (b *Bot) StartSoundLoop() {
	log.Printf(
		"Starting randomized sound loop. Target channel: %s",
		b.Config.VoiceChannelID,
	)

	for {
		// Calculate random delay to wait for next sound
		delay := b.randomSoundGap()
		log.Printf("Next sound in %v", delay)

		// Wait for the timer OR a potential stop signal
		select {
		case <-b.ctx.Done():
			log.Println("Sound loop received the stop signal. Exiting...")
			return
		case <-time.After(delay):
			// Pick a sound
			soundID := b.SoundManager.GetRandomID()
			if soundID == "" {
				log.Println("No sounds available to play. Skipping...")
				continue
			}
			// Trigger the sound
			// NOTE: the bot must be in the voice channel for this to work
			b.PlaySoundGrouping(soundID)
		}
	}
}

// PlaySoundGrouping plays a sound or rapid-fire repetition of sounds.
// It chooses whether to play a single sound or rapid fire probabilistically.
func (b *Bot) PlaySoundGrouping(soundID string) {
	if b.Config.RapidFireProbability < tolerance ||
		rand.Float32() > b.Config.RapidFireProbability {
		if err := b.PlaySoundboardSound(soundID); err != nil {
			log.Printf("Single shot sound playback error: %v", err)
		}
		return
	}
	b.PlayRapidFireSound(soundID)
}

// PlayRapidFireSound plays a soundboard sound rapidly and repeatedly.
// Takes the soundboard sound ID as input, and determines the number of plays
// and gap between plays randomly within a range specified in the bot's Config.
func (b *Bot) PlayRapidFireSound(soundID string) {
	min := b.Config.RapidFireCountMin
	max := b.Config.RapidFireCountMax
	count := min + rand.IntN(max-min+1)
	min = b.Config.RapidFireMinInterval
	max = b.Config.RapidFireMaxInterval
	interval := min + rand.IntN(max-min+1)
	for range count {
		// Use a goroutine to avoid latency of request handling
		go func() {
			if err := b.PlaySoundboardSound(soundID); err != nil {
				log.Printf("Rapid fire burst error: %v", err)
			}
		}()

		time.Sleep(time.Duration(interval) * time.Millisecond)
	}
}

// PlaySoundboardSound plays a sound a single time given its sound ID.
// Also returns an error if the API request to play the sound failed.
func (b *Bot) PlaySoundboardSound(soundID string) error {
	endpoint := discordgo.EndpointAPI +
		fmt.Sprintf("channels/%s/send-soundboard-sound", b.Config.VoiceChannelID)

	// Construct the payload to send to the Discord API
	payload := struct {
		SoundID string `json:"sound_id"`
	}{
		SoundID: soundID,
	}

	b.vc.Speaking(true)
	defer b.vc.Speaking(false)
	_, err := b.Session.Request("POST", endpoint, payload)
	if err != nil {
		return fmt.Errorf("failed to trigger soundboard: %w", err)
	}
	return nil
}

// randomSoundGap calculates a random gap between rapid-fire sound plays.
// The gap is between the range specified in the bot's Config.
func (b *Bot) randomSoundGap() time.Duration {
	b.mu.RLock()
	min := b.Config.MinSoundInterval
	max := b.Config.MaxSoundInterval
	b.mu.RUnlock()
	return shared.RandomDuration(min, max)
}
