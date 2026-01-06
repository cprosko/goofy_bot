package main

// Standard packages
import (
	// Internal Packages
	"goofybot/bot"
	"goofybot/shared"

	// Standard packages
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
	conf, err := shared.ParseConfig(configPath)
	if err != nil {
		log.Fatal("Error loading config.yaml,", err)
	}
	log.Printf("Config:\n%+v\n", conf)

	myBot, err := bot.InitializeBot(conf, ctx)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	defer myBot.Close()

	<-ctx.Done()
	log.Println("Shutting down...")
}
