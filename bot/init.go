/*
bot implements the main logic for running the Discord bot.

It tracks events on the connected Discord server and responds to them, manages
any dynamically changing part of the bot's state.
*/
package bot

import (
	// Internal packages ---------------------------------------------------------

	// Provides data structures and logic for configuration and soundboard sounds
	"goofybot/shared"

	// Standard packages ---------------------------------------------------------

	// Provides Context for handling safe closure of the program 
	"context"
	// Allows creating formatted error objects
	"fmt"
	// For printing errors and messages to the log
	"log"
	// For nontrivial string parsing of commands input to Discord's text chat
	"strings"
	// Provides Mutexes for concurrent handling of the bot state
	"sync"
	// Provides functionality for waiting/sleeping
	"time"

	// External packages ---------------------------------------------------------

	// Provides interface with Discord's API for bots
	"github.com/bwmarrin/discordgo"
)

// Bot handles all variables relating to the bot state and connection to Discord
type Bot struct {
	// Handles interfacing with Discord API and overall connection state
	Session           *discordgo.Session
	// Contains all static configuration parameters of the bot
	Config            *shared.Config
	// Concurrency-safe list of available soundboard sound information
	SoundManager      *shared.SoundManager
	// Slice of ALL the server's custom soundboard sound IDs
	CustomSounds      []string
	// Slice of ALL the server's default soundboard sound IDs
	DefaultSounds     []string
	// Cache mapping synthesized voice text to its audio clip filepath
	vocalCache        map[string]string
	// Map tracking when bot last reacted with voice to different events
	lastResponseTimes map[string]time.Time
	// Tracks if connection has already been made to voice channel
	loopRunning       bool
	// Mutex ensuring bot doesn't try to speak with overlap
	speakingMu        sync.Mutex
	// Handles interfacing with the specific connected voice channel
	vc                *discordgo.VoiceConnection
	// Mutex handling general safe access of bot members
	mu                sync.RWMutex
	// Context ensuring safe shutdown of the bot
	ctx               context.Context
}

// InitializeBot returns running bot given an input config and shutdown context.
// Also returns an error if a Discord session could not successfully start.
func InitializeBot(conf *shared.Config, ctx context.Context) (*Bot, error) {
	session, err := discordgo.New("Bot " + conf.Token)
	if err != nil {
		return nil, fmt.Errorf("Could not create Session: %w", err)
	}
	bot := &Bot{
		Session:           session,
		Config:            conf,
		SoundManager:      &shared.SoundManager{AvailableIDs: []string{}},
		CustomSounds:      []string{},
		DefaultSounds:     []string{},
		vocalCache:        make(map[string]string),
		lastResponseTimes: make(map[string]time.Time),
		loopRunning:       false,
		ctx:               ctx,
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

// JoinVoiceChannel causes the Bot to connect to a voice channel.
// The voice channel joined is specified by Bot.Config.VoiceChannelID.
// Also returns an error if the connection couldn't be established.
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

// registerHandlers adds functions to execute on certain Discord events.
// These include: when the server connection is established, when a user joins/
// leaves the channel, when text messages are received in the channel chat, and
// when the soundboard changes.
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

// handleVoiceStateUpdate provides logic to execute when user joins/leaves the
// channel.
// Currently, this causes the bot to probabilistically respond with a voice
// clip.
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

		b.mu.RLock()
		if time.Since(b.lastResponseTimes["joined"]) < b.Config.Cooldown {
			b.mu.RUnlock()
			return
		}
		b.lastResponseTimes["joined"] = time.Now()
		b.mu.RUnlock()

		b.maybeRespond(b.Config.ResponseProbability, "joined")
	} else if left && !isInChannel {
		log.Printf("User %s left the channel", v.UserID)
		
		b.mu.RLock()
		if time.Since(b.lastResponseTimes["left"]) < b.Config.Cooldown {
			b.mu.RUnlock()
			return
		}
		b.mu.RUnlock()

		b.maybeRespond(b.Config.ResponseProbability, "left")
	}
}

// handleMessage is executed when a message is typed in the chat.
// Currently, it parses for the !refresh and !say commands, which refresh the
// bot's soundboard information and force it to synthesize and speak a phrase,
// respectively.
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

// interpretEvent is executed for any API event, updating the soundboard.
// The function simply returns if the API event does not correspond to a change
// in the soundboard.
func (b *Bot) interpretEvent(event *discordgo.Event) {
	if event.Type == "GUILD_SOUNDBOARD_SOUND_CREATE" ||
		event.Type == "GUILD_SOUNDBOARD_DELETE" {
		log.Printf("Updating sound list due to Discord event %v\n", event.Type)
		b.RefreshSounds()
	}
}

// Close is an alias for closing the Discord session via the Bot object.
func (b *Bot) Close() {
	log.Println("Shutting down bot session...")
	b.Session.Close()
}
