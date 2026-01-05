package main

import (
	// Standard Packages
	"context"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"sync"
	"time"

	// External Packages
	"github.com/bwmarrin/discordgo"
	"github.com/pion/opus"
	"github.com/pion/webrtc/v3/pkg/media/oggreader"
)

const (
	tolerance    float32 = 0.001
	sampleRate           = 48000
	channels             = 2
	frameSize            = 960 // 20ms @48kHz
	silenceAfter         = 600 * time.Millisecond
	energyThresh         = 500.0
	// TODO: refactor sound generation command into constants here
)

type Bot struct {
	Session       *discordgo.Session
	Config        *Config
	SoundManager  *SoundManager
	CustomSounds  []string
	DefaultSounds []string
	VocalCache    map[string]string
	loopRunning   bool
	isSpeaking    bool
	vc            *discordgo.VoiceConnection
	mu            sync.RWMutex
	ctx           context.Context
	ssrcToUser    map[uint32]string
	vadMu         sync.Mutex
	vadStates     map[string]*vadState
}

type vadState struct {
	speaking  bool
	lastVoice time.Time
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
		VocalCache:    make(map[string]string),
		loopRunning:   false,
		isSpeaking:    false,
		ctx:           ctx,
		ssrcToUser:    make(map[uint32]string),
		vadStates:     make(map[string]*vadState),
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
	bot.PreGenerateTTS()

	// Begin bot session, join voice channel and add handlers
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

	// Enable voice receive: required for Discord to allow *sending* sound
	go b.startVoiceReceive()

	vc.AddHandler(
		func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if vs.Speaking {
				b.ssrcToUser[uint32(vs.SSRC)] = vs.UserID
			} else {
				delete(b.ssrcToUser, uint32(vs.SSRC))
			}
		},
	)

	log.Println("Voice connection established and handler registered.")
	return nil
}

func (b *Bot) startVoiceReceive() {
	log.Println("[VAD] Voice receive loop started")

	decoder := opus.NewDecoder()

	pcm := make([]byte, frameSize * channels)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case pkt, ok := <-b.vc.OpusRecv:
			if !ok || pkt == nil {
				return
			}

			n, _, err := decoder.Decode(pkt.Opus, pcm)
			if err != nil || n == 0 {
				continue
			}

			energy := pcmEnergy(pcm[:n*channels])
			userID, exists := b.ssrcToUser[uint32(pkt.SSRC)]
			if !exists {
				continue
			}
			b.processVAD(userID, energy)

		case <-ticker.C:
			b.checkForSpeechEnd()
		}
	}
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
		b.mu.RLock()
		b.Session.ChannelMessageSend(msg.ChannelID,
			b.Config.CommandResponses["refresh"])
		b.mu.RUnlock()
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
	b.mu.RLock()
	defer b.mu.RUnlock()
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

func (b *Bot) PreGenerateTTS() {
	log.Println("Pre-generating Opus sound files...")
	b.mu.Lock()
	b.VocalCache = make(map[string]string)
	b.mu.Unlock()
	_ = os.Mkdir("./cache", 0755)

	for i, text := range b.Config.Responses {
		path := fmt.Sprintf("./cache/response_%d.opus", i)

		// Pipeline: Piper -> FFmpeg (raw Opus stream)
		cmdStr := fmt.Sprintf("echo %q | piper --model %s --output-raw | "+
			"ffmpeg -f s16le -ar 22050 -ac 1 -i pipe:0 -c:a libopus -ar 48000 "+
			"-page_duration 20000 -ac 2 %s", text, b.Config.VoiceModel, path)

		if err := exec.Command("bash", "-c", cmdStr).Run(); err != nil {
			log.Printf("Failed to generate %s: %v", path, err)
			continue
		}

		b.mu.Lock()
		b.VocalCache[text] = path
		b.mu.Unlock()
	}
}

func (b *Bot) Speak(text string) error {
	log.Printf("Responding to voice with response: %s", text)
	b.mu.RLock()
	path, exists := b.VocalCache[text]
	b.mu.RUnlock()
	if !exists {
		return fmt.Errorf("No cache for: %s", text)
	}

	b.mu.Lock()
	b.isSpeaking = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.isSpeaking = false
		b.mu.Unlock()
	}()

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		return fmt.Errorf("Failed to create ogg reader: %w", err)
	}

	// Important: Small delay to ensure voice connection is ready to stream
	b.vc.Speaking(true)
	defer b.vc.Speaking(false)

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		// ParseNextPage retuns raw Opus packets from one Ogg page
		payload, _, err := ogg.ParseNextPage()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("Error reading ogg page: %w", err)
		}

		// Check headers
		if len(payload) > 8 &&
			(string(payload[:8]) == "OpusHead" || string(payload[:8]) == "OpusTags") {
			continue
		}

		// Wait for frame duration, equal to Discord heartbeat
		select {
		case <-ticker.C:
			b.vc.OpusSend <- payload
		case <-b.ctx.Done():
			return nil
		}
	}
	return nil
}

func (b *Bot) processVAD(userID string, energy float64) {
	if userID == b.Session.State.User.ID {
		return
	}

	now := time.Now()

	b.vadMu.Lock()
	defer b.vadMu.Unlock()

	state, exists := b.vadStates[userID]
	if !exists {
		state = &vadState{}
		b.vadStates[userID] = state
	}

	if energy > energyThresh {
		if !state.speaking {
			log.Printf("[VAD] User %s started speaking", userID)
			state.speaking = true
		}
		state.lastVoice = now
	}
}

func (b *Bot) checkForSpeechEnd() {
	now := time.Now()

	b.vadMu.Lock()
	defer b.vadMu.Unlock()
	for userID, state := range b.vadStates {
		if state.speaking && now.Sub(state.lastVoice) > silenceAfter {
			state.speaking = false
			log.Printf("[VAD] User %s finished speaking", userID)

			go b.maybeRespond()
		}
	}
}

func (b *Bot) maybeRespond() {
	b.mu.RLock()
	if b.isSpeaking {
		b.mu.RUnlock()
		return
	}
	prob := b.Config.ResponseProbability
	b.mu.RUnlock()

	roll := rand.Float32()
	log.Printf("Probability roll: %v (Needs to be < %v)", roll, prob)
	if roll < prob {
		log.Println("Probability check passed! Responding...")
		go b.respondWithTTS()
	}
}

func (b *Bot) respondWithTTS() {
	b.mu.RLock()
	responses := b.Config.Responses
	b.mu.RUnlock()

	if len(responses) == 0 {
		return
	}

	response := responses[rand.IntN(len(responses))]

	if err := b.Speak(response); err != nil {
		log.Printf("TTS error: %v", err)
	}
}

func pcmEnergy(samples []byte) float64 {
	var sum float64
	for _, s := range samples {
		sum += float64(int16(s) * int16(s))
	}
	return sum / float64(len(samples))
}
