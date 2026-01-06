package bot

import (
	// Internal packages
	"goofybot/shared"

	// Standard packages
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	// External packages
	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Session       *discordgo.Session
	Config        *shared.Config
	SoundManager  *shared.SoundManager
	CustomSounds  []string
	DefaultSounds []string
	VocalCache    map[string]string
	loopRunning   bool
	isSpeaking    bool
	vc            *discordgo.VoiceConnection
	mu            sync.RWMutex
	ctx           context.Context
}

func InitializeBot(conf *shared.Config, ctx context.Context) (*Bot, error) {
	session, err := discordgo.New("Bot " + conf.Token)
	if err != nil {
		return nil, fmt.Errorf("Could not create Session: %w", err)
	}
	bot := &Bot{
		Session:       session,
		Config:        conf,
		SoundManager:  &shared.SoundManager{AvailableIDs: []string{}},
		CustomSounds:  []string{},
		DefaultSounds: []string{},
		VocalCache:    make(map[string]string),
		loopRunning:   false,
		isSpeaking:    false,
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

	// Pregenerate response audio
	bot.preGenerateTTS()

	// Begin bot session, join voice channel and add handlers
	log.Printf(" Session OS / Browser: %s / %s",
		bot.Session.Identify.Properties.OS,
		bot.Session.Identify.Properties.Browser)
	bot.registerHandlers()
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

	// Wait for the connection to be fully ready (max 30 seconds)
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for !b.vc.Ready {
		select {
		case <-timeout:
			return fmt.Errorf("Voice connection timed out waiting for ready state")
		case <-ticker.C:
			// Still waiting...
		}
	}

	log.Println("Voice connection established and handler registered.")
	return nil
}

func (b *Bot) registerHandlers() {
	// Join channel on Session ready and server recognized
	b.Session.AddHandler(
		func(s *discordgo.Session, g *discordgo.GuildCreate) {
			// Check this is the guild from the config
			if g.ID != b.Config.ServerID {
				return
			}

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
			go b.StartSoundLoop() // Random soundboard sounds
			go b.StartVoiceLoop() // Random voice responses
		},
	)
	// React when user joins/leaves channel
	b.Session.AddHandler(
		func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
			b.handleVoiceStateUpdate(v)
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
}

func (b *Bot) handleVoiceStateUpdate(v *discordgo.VoiceStateUpdate) {
	// Ignore bot itself
	if v.UserID == b.Session.State.User.ID {
		return
	}

	// Determine if user joined or left the bot's channel
	joined := v.BeforeUpdate == nil ||
		v.BeforeUpdate.ChannelID != b.Config.VoiceChannelID
	left := v.BeforeUpdate != nil &&
		v.BeforeUpdate.ChannelID == b.Config.VoiceChannelID
	isInChannel := v.ChannelID == b.Config.VoiceChannelID

	if joined && isInChannel {
		log.Printf("User %s joined the channel", v.UserID)
		b.maybeRespond(b.Config.ResponseProbability, "joined")
	} else if left && !isInChannel {
		log.Printf("User %s left the channel", v.UserID)
		b.maybeRespond(b.Config.ResponseProbability, "left")
	}
}

func (b *Bot) handleMessage(msg *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if msg.Author.ID == b.Session.State.User.ID {
		return
	}
	content := msg.Content
	switch {
	case strings.HasPrefix(content, "!refresh"):
		// refresh soundboard
		log.Printf("Refresh command received from user: %s", msg.Author.Username)
		b.RefreshSounds()
		b.mu.RLock()
		b.Session.ChannelMessageSend(msg.ChannelID,
			b.Config.CommandResponses["refresh"])
		b.mu.RUnlock()
	case strings.HasPrefix(content, "!say "):
		text := string(content[5:])
		_, err := b.generateTTSAndGetPath(text)
		if err != nil {
			log.Printf("Error generating audio for !say command with text %s: %v",
				text, err)
		}
		b.Speak(text)
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
