package main

import (
	// Standard Packages
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	// External Packages
	"github.com/bwmarrin/discordgo"
)

const tolerance float32 = 0.001

type Bot struct {
	Session       *discordgo.Session
	Config        *Config
	SoundManager  *SoundManager
	CustomSounds  []string
	DefaultSounds []string
	loopRunning   bool
	vc            *discordgo.VoiceConnection
	mu            sync.RWMutex
	ctx           context.Context
}

func InitializeBot(conf *Config, ctx context.Context) (*Bot, error) {
	session, err := discordgo.New("Bot " + conf.Token)
	if err != nil {
		return nil, fmt.Errorf("Could not create Session: %w", err)
	}
	bot := &Bot{
		Session:       session,
		Config:        conf,
		SoundManager:  &SoundManager{AvailableIDs: []string{}},
		CustomSounds:  []string{},
		DefaultSounds: []string{},
		loopRunning:   false,
		ctx:           ctx,
	}
	// In order: join voice channel and track who is in it, receive soundboard
	// notification events, listen to channel text messages, and see message
	// content. IntentsMessageContent must also be activated on Developer Portal
	bot.Session.Identify.Intents |= discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent
	// IntentsMessageContent must also be activated in Developer Portal
	bot.RegisterHandlers()
	err = bot.Session.Open()
	if err != nil {
		return bot, fmt.Errorf("Could not open Session: %w", err)
	}
	return bot, nil
}

func (b *Bot) JoinVoiceChannel() error {
	vc, err := b.Session.ChannelVoiceJoin(
		b.Config.ServerID,
		b.Config.VoiceChannelID,
		false, // bot is unmuted
		false, // bot is not 'deaf' to voice
	)
	if err != nil {
		return fmt.Errorf("Failed to join voice channel: %w", err)
	}
	b.vc = vc

	return nil
}

func (b *Bot) RefreshSounds() {
	customSounds, err := fetchGuildSounds(b.Session, b.Config.ServerID)
	if err != nil {
		log.Printf("Error fetching custom sounds: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.CustomSounds = availableSounds(customSounds, b.Config)

	var finalPool []string
	if b.Config.UseDefaultSounds {
		// Only fetch defaults if we haven't already
		if len(b.DefaultSounds) == 0 {
			defaultSounds, err := fetchDefaultSounds(b.Session, b.Config.ServerID)
			if err != nil {
				log.Printf("Error fetching default sounds: %v", err)
			} else {
				b.DefaultSounds = availableSounds(defaultSounds, b.Config)
			}
		}
		finalPool = append(b.CustomSounds, b.DefaultSounds...)
	} else {
		finalPool = b.CustomSounds
	}

	b.SoundManager.UpdateIDs(finalPool)
	log.Printf("Sounds refreshed. Total pool size: %d", len(finalPool))
}

func (b *Bot) RegisterHandlers() {
	// Join channel on Session ready and server recognized
	b.Session.AddHandler(
		func(s *discordgo.Session, g *discordgo.GuildCreate) {
			// Check this is the guild from the config
			if g.ID != b.Config.ServerID {
				return
			}
			log.Printf("Guild available: %s", g.Name)

			// Ensure we only start one loop
			b.mu.Lock()
			if b.loopRunning {
				b.mu.Unlock()
				return
			}
			b.loopRunning = true
			b.mu.Unlock()

			if err := b.JoinVoiceChannel(); err != nil {
				log.Printf("Join error: %v", err)
			}
			b.RefreshSounds()
			go b.StartSoundLoop()
		},
	)
	// Listen to messages in the text channel
	b.Session.AddHandler(
		func(s *discordgo.Session, m *discordgo.MessageCreate) {
			b.handleMessage(m)
		},
	)
	// Listen for any change to the soundboard
	b.Session.AddHandler(
		func(s *discordgo.Session, e *discordgo.Event) {
			b.interpretEvent(e)
		},
	)
	// Listen for voice activity and respond with text-to-speech
	b.Session.AddHandler(
		func(s *discordgo.Session, v *discordgo.VoiceSpeakingUpdate) {
			b.handleVoiceMessage(v)
		},
	)
}

func (b *Bot) handleMessage(msg *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if msg.Author.ID == b.Session.State.User.ID {
		return
	}
	// '!refresh': refresh soundboard
	if msg.Content == "!refresh" {
		log.Printf("Refresh command received from user: %s", msg.Author.Username)
		b.RefreshSounds()
		b.Session.ChannelMessageSend(msg.ChannelID,
			b.Config.CommandResponses["refresh"])
	}
}

func (b *Bot) interpretEvent(event *discordgo.Event) {
	if event.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" ||
		event.Type == "GUILD_SOUNDBOARD_DELETE" {
		log.Printf("Updating sound list due to Discord event %v\n", event.Type)
		b.RefreshSounds()
	}
}

func (b *Bot) handleVoiceMessage(v *discordgo.VoiceSpeakingUpdate) {
	// Ignore the bot itself
	if v.UserID == b.Session.State.User.ID {
		return
	}

	// v.Speaking is 1 if they started talking and 0 if they stopped
	if v.Speaking == true {
		if rand.Float32() < b.Config.ResponseProbability {
			go b.respondWithTTS()
		}
	}
}

func (b *Bot) respondWithTTS() {
	// TODO: Not implemented
}

func (b *Bot) Close() {
	log.Println("Shutting down bot session...")
	b.Session.Close()
}

func (b *Bot) StartSoundLoop() {
	log.Printf(
		"Starting randomized sound loop. Target channel: %s",
		b.Config.VoiceChannelID,
	)

	for {
		// Calculate random delay to wait for next sound
		delay := b.getRandomDuration()
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

func (b *Bot) getRandomDuration() time.Duration {
	min := b.Config.MinInterval
	max := b.Config.MaxInterval
	seconds := rand.IntN(max-min+1) + min
	return time.Duration(seconds) * time.Second
}

func (b *Bot) PlaySoundGrouping(soundID string) {
	if b.Config.RapidFireProbability < tolerance ||
		rand.Float32() > b.Config.RapidFireProbability {
		if err := b.PlaySoundboardSound(soundID); err != nil {
			log.Printf("Single shot sound playback error: %v", err)
		}
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
	endpoint := discordgo.EndpointAPI + fmt.Sprintf("channels/%s/send-soundboard-sound", b.Config.VoiceChannelID)

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
