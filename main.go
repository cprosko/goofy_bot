package main

// Standard packages
import (
	"context"
	"log"
	"os"
	"os/signal"
)

const configPath string = "./config.yaml"

func main() {
	// Set up logger
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Create a context cancelled when process receives interrupt
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Load configuration
	conf, err := parseConfig(configPath)
	if err != nil {
		log.Fatal("Error loading config.yaml,", err)
	}
	log.Printf("Config:\n%+v\n", conf)

	bot, err := InitializeBot(conf)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	defer bot.Close()

	err = bot.JoinVoiceChannel()
	if err != nil {
		log.Fatalf("Failed to join voice channel: %v", err)
	}

	go bot.StartSoundLoop(ctx)

	<-ctx.Done()
	log.Println("Shutting down...")
}
