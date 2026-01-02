package main

// Standard packages
import (
	"fmt"
	"log"
)

const configPath string = "./config.yaml"


func main() {
	// Load configuration
	conf, err := parseConfig(configPath)
	if err != nil {
		log.Fatal("Error loading config.yaml,", err)
	}
	fmt.Printf("Config:\n%+v\n", conf)
	bot, err := initializeBot(conf)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	defer bot.Close()

	// bot.startSoundLoop()

	fmt.Println("TODO")
}
