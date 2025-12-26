package main

// Standard packages
import (
	// Standard Packages
	"fmt"
	"log"
	"os"

	// External Packages
	"github.com/bwmarrin/discordgo"
)

const configPath string = "./config.yaml"


func main() {
	// Load configuration
	token := os.Getenv("DISCORD_BOT_TOKEN")
	conf := parseConfig(configPath)

	// Create session
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Error creating Discord session, ", err)
	}

	// Set bot intents
	session.Identify.Intents = discordgo.IntentsGuildVoiceStates

	// Open connection
	err = session.Open()
	if err != nil {
		log.Fatal("Error opening connection,", err)
	}
	defer session.Close()

	// TODO: random sound interval logic

	fmt.Println("TODO")
}
