module goofybot

go 1.25.5

// Direct dependencies
require (
	github.com/bwmarrin/discordgo v0.29.0 // direct
	gopkg.in/yaml.v3 v3.0.1 // direct
)

replace github.com/bwmarrin/discordgo => github.com/darui3018823/discordgo v0.29.0-patched

// Indirect dependencies
require (
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

require github.com/darui3018823/discordgo v0.29.0-patched-2 // indirect
