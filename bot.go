package main

import (
	// Standard Packages
	"fmt"
	"log"

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

func initializeBot(conf *Config) (*Bot, error) {
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
	bot.Session.Identify.Intents = discordgo.IntentsGuildVoiceStates
	err = bot.Session.Open()
	if err != nil {
		return bot, fmt.Errorf("Could not open Session: %w", err)
	}
	bot.refreshSounds()

	return bot, nil
}

func (b *Bot) Close() {
	log.Println("Shutting down bot session...")
	b.Session.Close()
}

func (b *Bot) refreshSounds() {
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

func (b *Bot) startSoundLoop() error {
	// Initially fetch all available sounds
	b.refreshSounds()

	// Set up ticker for periodically refreshing sounds
	return nil
}
