package main

import (
	// Standard Packages
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"time"

	// External Packages
	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Session       *discordgo.Session
	Config        *Config
	SoundManager  *SoundManager
	CustomSounds  []string
	DefaultSounds []string
}

func InitializeBot(conf *Config) (*Bot, error) {
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
	}
	// In order: join voice channel and track who is in it, receive soundboard
	// notification events, listen to channel text messages, and see message
	// content. IntentsMessageContent must also be activated on Developer Portal
	bot.Session.Identify.Intents = discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent
	// IntentsMessageContent must also be activated in Developer Portal
	err = bot.Session.Open()
	if err != nil {
		return bot, fmt.Errorf("Could not open Session: %w", err)
	}
	bot.RefreshSounds()
	bot.RegisterHandlers()
	return bot, nil
}

func (b *Bot) JoinVoiceChannel() error {
	_, err := b.Session.ChannelVoiceJoin(
		b.Config.ServerID,
		b.Config.VoiceChannelID,
		false, // bot is unmuted
		true,  // bot is 'deaf' to voice
	)
	if err != nil {
		return fmt.Errorf("Failed to join voice channel: %w", err)
	}
	return nil
}

func (b *Bot) RefreshSounds() {
	customSounds, err := fetchGuildSounds(b.Session, b.Config.ServerID)
	if err != nil {
		log.Printf("Error fetching custom sounds: %v", err)
		return
	}
	b.CustomSounds = availableSounds(customSounds, b.Config)
	if !b.Config.UseDefaultSounds {
		b.SoundManager.UpdateIDs(b.CustomSounds)
		return
	}
	if len(b.DefaultSounds) == 0 {
		defaultSounds, err := fetchDefaultSounds(b.Session, b.Config.ServerID)
		if err != nil {
			log.Printf("Error fetching default sounds: %v", err)
			return
		}
		b.DefaultSounds = availableSounds(defaultSounds, b.Config)
	}
	b.SoundManager.UpdateIDs(append(b.CustomSounds, b.DefaultSounds...))
}

func (b *Bot) RegisterHandlers() {
	// Listen to messages in the text channel
	b.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		b.handleMessage(m)
	})
	// Listen for any change to the soundboard
	b.Session.AddHandler(func(s *discordgo.Session, e *discordgo.Event) {
		b.interpretEvent(e)
	})
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
		b.Session.ChannelMessageSend(msg.ChannelID, b.Config.Responses["refresh"])
	}
}

func (b *Bot) interpretEvent(event *discordgo.Event) {
	if event.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" ||
		event.Type == "GUILD_SOUNDBOARD_DELETE" {
		log.Printf("Updating sound list due to Discord event %v\n", event.Type)
		b.RefreshSounds()
	}
}

func (b *Bot) Close() {
	log.Println("Shutting down bot session...")
	b.Session.Close()
}

func (b *Bot) StartSoundLoop(ctx context.Context) {
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
		case <-ctx.Done():
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
			err := b.PlaySoundboardSound(soundID)
			if err != nil {
				log.Printf("Error playing soundboard sound: %v", err)
			} else {
				log.Printf("Successfully triggered sound: %s", soundID)
			}
		}
	}
}

func (b *Bot) getRandomDuration() time.Duration {
	min := b.Config.MinInterval
	max := b.Config.MaxInterval
	seconds := rand.IntN(max-min+1) + min
	return time.Duration(seconds) * time.Second
}

func (b *Bot) PlaySoundboardSound(soundID string) error {
	endpoint := discordgo.EndpointChannel(b.Config.VoiceChannelID) +
		"/send-soundboard-sound"

	// Construct the payload to send to the Discord API
	payload := struct {
		SoundID string `json:"sound_id"`
	}{
		SoundID: soundID,
	}

	_, err := b.Session.Request("POST", endpoint, payload)
	if err != nil {
		return fmt.Errorf("failed to trigger soundboard: %w", err)
	}
	return nil
}
