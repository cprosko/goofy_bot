package bot

import (
	// Internal packages ---------------------------------------------------------

	// Provides data structures and logic for configuration and soundboard sounds
	"goofybot/shared"

	// Standard packages ---------------------------------------------------------

	// For creating buffer for remembering stderr for executed shell commands
	"bytes"
	// For generating unique hashes for each generated voice clip
	"crypto/sha256"
	// For converting hashes into strings
	"encoding/hex"
	// Allows creating formatted error objects
	"fmt"
	// Enables checking for the end of read files
	"io"
	// For printing errors and messages to the log
	"log"
	// For probabilistically responding with voice and choosing responses 
	"math/rand/v2"
	// Enables making/deleting files and directories, and reading audio files.
	"os"
	// Enables running shell commands for generating audio
	"os/exec"
	// Provides functionality for waiting/sleeping
	"time"

	// External packages ---------------------------------------------------------

	// Enables parsing generated audio files for streaming to Discord as voice
	"github.com/pion/webrtc/v3/pkg/media/oggreader"
)

// Parameters determining Piper voice synthesis and audio generation
const (
	audioRate               int    = 48000   // Discord standard audio rate
	piperFormat             string = "s16le" // Standard output format from Piper
	piperRate               int    = 22050   // Rate for most 'medium' quality Piper models
	compressionLevel        int    = 10      // Compression used by Piper
	bitRate                 int    = 64      // Bitrate for Piper audio output, in kBits/s
	piperChannels           int    = 1       // Number of audio channels Piper generates
	outputChannels          int    = 2       // Number of audio channels Discord sound
	packetLengthNanoseconds int    = 20000   // Audio chunk size needed by Discord
)

// StartVoiceLoop begins infinite loop of randomly playing voice clips.
// Should be run as a goroutine.
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
		delay := b.randomVoiceGap()
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

// randomVoiceGap returns a random Duration before playing next voice clip.
func (b *Bot) randomVoiceGap() time.Duration {
	b.mu.RLock()
	min := b.Config.MinVoiceInterval
	max := b.Config.MaxVoiceInterval
	b.mu.RUnlock()
	return shared.RandomDuration(min, max)
}

// preGenerateTTS generates voice clips with Piper or finds cached voice clip.
// It stores a map from the message's text to the filepath of the related clip
// in Bot.VocalCache. It also deletes any sound clips in ./cache/ which do not
// correspond to any potential voice clip in the Bot.Config.
func (b *Bot) preGenerateTTS() {
	log.Println("Checking Existence of or pre-generating Opus sound files...")
	_ = os.Mkdir("./cache", 0755)

	activeFiles := make(map[string]struct{})

	b.mu.Lock()
	b.vocalCache = make(map[string]string)
	b.mu.Unlock()

	for _, responses := range b.Config.Responses {
		for _, resp := range responses {
			path, err := b.generateTTSAndGetPath(resp)
			if err != nil {
				fmt.Printf("Unable to generate audio for text \"%s\", full error:", err)
				continue
			}
			activeFiles[path] = struct{}{}
		}
	}

	// Clean files that are no longer relevant to the config
	b.cleanCache(activeFiles)
}

// getCachePath creates a unique audio file path for a given voice message.
// This enables a unique mapping from messages to their generated voice clips.
func (b *Bot) getCachePath(text string) string {
	hash := sha256.New()
	// We hash both the text and the model name
	hash.Write([]byte(text + b.Config.VoiceModel))
	token := hex.EncodeToString(hash.Sum(nil))
	return fmt.Sprintf("./cache/%s.opus", token)
}

// ceanCache deletes all audio files not listed in input activeFiles.
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

// Speak causes the bot to play the audio clip matching `text` to the channel.
// If a matching audio clip has not yet been generated, it returns an error.
// Also returns an error if the audio file couldn't be opened or parsed.
func (b *Bot) Speak(text string) error {
	log.Printf("About to say: %s", text)
	b.mu.RLock()
	path, exists := b.vocalCache[text]
	b.mu.RUnlock()
	if !exists {
		return fmt.Errorf("No cache for: %s", text)
	}

	b.speakingMu.Lock()
	defer b.speakingMu.Unlock()

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

// generateTTSAndGetPath synthesizes a voice clip saying the input text.
// It returns the generated clip's relative file path, and an error if the clip
// generation fails.
// It stores a map between the text and the filepath in the Bot's VocalCache
// member.
func (b *Bot) generateTTSAndGetPath(text string) (string, error) {
	path := b.getCachePath(text)

	// Skip generation if file exists
	if _, err := os.Stat(path); err == nil {
		log.Printf("Using cached file for: %q", text)
		b.mu.Lock()
		b.vocalCache[text] = path
		b.mu.Unlock()
		return path, nil
	}
	log.Printf("Generating new audio for: %q", text)
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
		return "", fmt.Errorf("Failed to create stdin pipe: %w", err)
	}

	// Start the command, expecting stdin as its input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("Failed to start command: %w", err)
	}

	// Write the response text directly to stdin
	fmt.Fprintln(stdin, text)
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("Failed to generate %s: %w\nFull Error: %s",
			path, err, stderr.String())
	}

	b.mu.Lock()
	b.vocalCache[text] = path
	b.mu.Unlock()

	return path, nil
}

// maybeRespond responds with a voice clip given input probability and response
// category.
func (b *Bot) maybeRespond(probability float32, category string) {
	roll := rand.Float32()
	log.Printf("Probability roll: %v (Needs to be < %v)", roll, probability)
	if roll < probability {
		log.Println("Probability check passed! Responding...")
		b.mu.Lock()
		b.lastResponseTimes[category] = time.Now()
		b.mu.Unlock()
		go b.respondWithTTS(b.Config.Responses[category])
	}
}

// respondWithTTS speaks with a random response from the input list of responses.
// Logs an error and fails if an audio clip has not already been generated for
// the randomly selected response.
func (b *Bot) respondWithTTS(responses []string) {
	if len(responses) == 0 {
		return
	}

	response := responses[rand.IntN(len(responses))]

	if err := b.Speak(response); err != nil {
		log.Printf("TTS error: %v", err)
	}
}
