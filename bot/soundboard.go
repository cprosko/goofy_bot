package bot

import (
	// Internal packages
	"goofybot/shared"

	// Standard packages
	"fmt"
	"log"
	"math/rand/v2"
	"time"

	// External packages
	"github.com/bwmarrin/discordgo"
)

const tolerance float32 = 0.0001

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

func (b *Bot) randomSoundGap() time.Duration {
	b.mu.RLock()
	min := b.Config.MinSoundInterval
	max := b.Config.MaxSoundInterval
	b.mu.RUnlock()
	return shared.RandomDuration(min, max)
}
