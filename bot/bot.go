package bot

import (
	// Internal packages
	"goofybot/shared"

	// Standard packages
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"sync"
	"time"

	// External packages
	"github.com/bwmarrin/discordgo"
	"github.com/pion/webrtc/v3/pkg/media/oggreader"
)

const (
	tolerance               float32 = 0.001
	audioRate               int     = 48000   // Discord standard audio rate
	piperFormat             string  = "s16le" // Standard output format from Piper
	piperRate               int     = 22050   // Rate for most 'medium' quality Piper models
	compressionLevel        int     = 10      // Compression used by Piper
	bitRate                 int     = 64      // Bitrate for Piper audio output, in kBits/s
	piperChannels           int     = 1       // Number of audio channels Piper generates
	outputChannels          int     = 2       // Number of audio channels Discord sound
	packetLengthNanoseconds int     = 20000   // Audio chunk size needed by Discord
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

type vadState struct {
	speaking  bool
	lastVoice time.Time
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
	bot.PreGenerateTTS()

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
		delay := b.getRandomSoundGap()
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

func (b *Bot) StartVoiceLoop() {
	log.Printf(
		"Starting randomized voice response loop. Target channel: %s",
		b.Config.VoiceChannelID,
	)

	b.mu.RLock()
	responses := b.Config.Responses["random"]
	b.mu.RUnlock()

	if len(responses) == 0 {
		log.Printf("Now voice responses available, aborting voice loop...")
		return
	}

	for {
		// Calculate random delay to wait for next sound
		delay := b.getRandomVoiceGap()
		log.Printf("Next voice message in %v", delay)

		// Wait for the timer OR a potential stop signal
		select {
		case <-b.ctx.Done():
			log.Println("Sound loop received the stop signal. Exiting...")
			return
		case <-time.After(delay):
			// Pick a response
			text := responses[rand.IntN(len(responses))]
			// Trigger the voice response
			// NOTE: the bot must be in the voice channel for this to work
			b.Speak(text)
		}
	}
}

func (b *Bot) getRandomSoundGap() time.Duration {
	b.mu.RLock()
	min := b.Config.MinSoundInterval
	max := b.Config.MaxSoundInterval
	b.mu.RUnlock()
	return getRandomDuration(min, max)
}

func (b *Bot) getRandomVoiceGap() time.Duration {
	b.mu.RLock()
	min := b.Config.MinVoiceInterval
	max := b.Config.MaxVoiceInterval
	b.mu.RUnlock()
	return getRandomDuration(min, max)
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
	log.Println("Checking Existence of or pre-generating Opus sound files...")
	_ = os.Mkdir("./cache", 0755)

	activeFiles := make(map[string]struct{})

	b.mu.Lock()
	b.VocalCache = make(map[string]string)
	b.mu.Unlock()

	for _, responses := range b.Config.Responses {
		for _, resp := range responses {
			path := b.getCachePath(resp)
			activeFiles[path] = struct{}{}

			// Skip generation if file exists
			if _, err := os.Stat(path); err == nil {
				log.Printf("Using cached file for: %q", resp)
				b.mu.Lock()
				b.VocalCache[resp] = path
				b.mu.Unlock()
				continue
			}
			log.Printf("Generating new audio for: %q", resp)
			// Pipeline: Piper -> FFmpeg (raw Opus stream)
			cmdStr := fmt.Sprintf("piper --model %s --output-raw | "+
				"ffmpeg -y -f %s -ar %v -ac %v -i pipe:0 -c:a libopus -ar %v "+
				"-page_duration %v -ac %v -compression_level %v -b:a %vk -vbr off %s",
				b.Config.VoiceModel, piperFormat, piperRate, piperChannels, audioRate,
				packetLengthNanoseconds, outputChannels, compressionLevel, bitRate, path,
			)
			cmd := exec.Command("bash", "-c", cmdStr)

			// Pipe to the command's stdin
			stdin, err := cmd.StdinPipe()
			if err != nil {
				log.Printf("Failed to create stdin pipe: %v", err)
				continue
			}

			// Start the command, expecting stdin as its input
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start command: %v", err)
				continue
			}

			// Write the response text directly to stdin
			fmt.Fprintln(stdin, resp)
			stdin.Close()

			if err := cmd.Wait(); err != nil {
				log.Printf("Failed to generate %s: %v\nFull Error: %s",
					path, err, stderr.String())
				continue
			}

			b.mu.Lock()
			b.VocalCache[resp] = path
			b.mu.Unlock()
		}
	}

	// Clean files that are no longer relevant to the config
	b.cleanCache(activeFiles)
}

func (b *Bot) getCachePath(text string) string {
	hash := sha256.New()
	// We hash both the text and the model name
	hash.Write([]byte(text + b.Config.VoiceModel))
	token := hex.EncodeToString(hash.Sum(nil))
	return fmt.Sprintf("./cache/%s.opus", token)
}

func (b *Bot) cleanCache(activeFiles map[string]struct{}) {
	files, err := os.ReadDir("./cache")
	if err != nil {
		log.Printf("Error cleaning cache: %v", err)
		return
	}

	for _, f := range files {
		path := "./cache/" + f.Name()
		if _, exists := activeFiles[path]; !exists {
			log.Printf("Removing old cache file: %s", path)
			os.Remove(path)
		}
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

	// Open cached audio file
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	ogg, _, err := oggreader.NewWith(file)
	if err != nil {
		return fmt.Errorf("Failed to create ogg reader: %w", err)
	}

	b.vc.Speaking(true)
	defer b.vc.Speaking(false)

	// Ensure Speaking(true) is processed before audio is sent
	time.Sleep(200 * time.Millisecond)

	// Send audio file packet-by-packet
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

func (b *Bot) maybeRespond(probability float32, category string) {
	b.mu.RLock()
	if b.isSpeaking {
		b.mu.RUnlock()
		return
	}
	b.mu.RUnlock()

	roll := rand.Float32()
	log.Printf("Probability roll: %v (Needs to be < %v)", roll, probability)
	if roll < probability {
		log.Println("Probability check passed! Responding...")
		go b.respondWithTTS(b.Config.Responses[category])
	}
}

func (b *Bot) respondWithTTS(responses []string) {
	if len(responses) == 0 {
		return
	}

	response := responses[rand.IntN(len(responses))]

	if err := b.Speak(response); err != nil {
		log.Printf("TTS error: %v", err)
	}
}

func getRandomDuration(min int, max int) time.Duration {
	seconds := rand.IntN(max-min+1) + min
	return time.Duration(seconds) * time.Second
}
