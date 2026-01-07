/*
goofybot connects and runs a Discord bot for playing silly voices and sounds.
It uses a config.yaml as configuration in the same directory and requires a
Discord bot token under DISCORD_BOT_TOKEN defined as an environment variable.
It also requires ffmpeg (for audio conversion) and Piper (for voice synthesis)
to be installed and available on $PATH.

See docstring for Config struct for details of configuring the bot.

Usage:

	./goofy_bot
	(after compiling the software with `go build -o goofy_bot`)
*/
package main

import (
	// Internal Packages ---------------------------------------------------------

	// Main bot logic
	"goofybot/bot"
	// Functionality relating to soundboard sounds and configuration
	"goofybot/shared"

	// Standard packages ---------------------------------------------------------

	// Provides Context object for coordinating the closure of the program
	"context"
	// For logging of the bot activity and errors
	"log"
	// For recognizing a program interrupt signal (e.g. ctrl+C)
	"os"
	// Provides special Context object signalling when a close signal is received
	// for the application
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
