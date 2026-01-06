# `goofy_bot`

A Discord bot written in Go with the [`discordgo`](https://github.com/darui3018823/discordgo) library which configurably plays random soundboard sounds.
It also synthesizes voice clips and plays them randomly, when users enter/leave the channel, or (WIP) on command.

## Installation & Usage

### Initial Installation

1. Install [Piper](https://github.com/rhasspy/piper) (for audio synthesis), [`ffmpeg`](https://www.ffmpeg.org/) (for audio conversion), and [Go](go.dev), and ensure they exist on `$PATH`.
2. Install the bot application on your voice channel with this [install link](https://discord.com/oauth2/authorize?client_id=1453886705625993379) or create your own bot application on the [Discord Developer Portal](https://discord.com/developers/). If creating your own bot, ensure the following:
  1. Under *Installation/Guild Install*, add the `bot` to Scopes.
  2. Under *Bot*, grant the application "Message Content Intent".
  3. Under *Installation/Installation Contexts*, ensure "Guild Install" is checked.
3. Clone this repository.
4. If using voice synthesis features, download a Piper voice model (see [here](https://huggingface.co/rhasspy/piper-voices/tree/main/en/en_US) for example) including the `.onnx` and `.onnx.json` files into your repository's root directory.
  * The code (`bot.go`) assumes a sample rate of 22050MHz, as is the case for most 'Medium' quality Piper models. This is specified on the `MODEL_CARD` file for the model you download. If your model doesn't match this, simply change the value of the `piperRate` constant in `bot.go`.
5. From the repository root directory, build the Go project with `go build -o goofy_bot`
6. Create a `config.yaml` configuration in the repository root directory, see `config.example.yaml` for a reference to its format and all parameters. See also the below Configuration section for more details.
  * Adjusting the `config.yaml` does *not* require rebuilding the Go files.
7. Get a Discord bot token if you don't have one from the [Developer Portal](https://discord.com/developers/)

### Usage

To start up the bot:
1. Ensure your bot token is available as an environment variable called `DISCORD_BOT_TOKEN` before running the executable. For example, run `export DISCORD_BOT_TOKEN=<your_bot_token>` (Bash).
2. Run the bot with `./goofy_bot` and it will automatically join the Server voice channel specified in the `config.yaml`.
3. Have fun!

The bot should automatically refresh the soundboard sounds when it changes, but you can force a manual refresh by sending the `!refresh` command to the voice channel chat.

## Configuration

Below, we explain all of the configuration parameters present in `config.example.yaml`. All of these parameters must appear in your `config.yaml`. For robustness, we encourage you to wrap all string parameters in `"` quotation marks. To see server and voice channel IDs as well as Soundboard sound IDs, enable developer mode in Discord by going to User Settings -> Advenced, and check Developer Mode. Now you can see the IDs of servers, voice channels, and soundboard sounds by right-clicking the server icon or voice channel name or sound icon and clicking the appropriate "Copy ..." option.

* `server_id`: The ID of your Discord Server/Guild.
* `voice_channel_id`: The ID of the voice channel you want the bot to join.
* `min_sound_interval_seconds`/`max_sound_interval_seconds`: The minimum/maximum number of seconds between the bot randomly playing a random soundboard sound.
* `rapid_fire_probability`: The probability of the bot playing a rapid sequence of the same sound when it plays a soundboard sound, expressed as a number between 0 and 1.
* `rapid_fire_min_interval_milliseconds`/`rapid_fire_max_interval_milliseconds`: When executing a 'rapid fire' of soundboard sounds, these are the minimum and maximum gaps between the sounds, with a value chosen randomly within this range.
* `rapid_fire_count_min`/`rapid_fire_count_max`: The minimum and maximum number of sounds to play in rapid succession when executing a 'rapid fire' of soundbooard sounds. The count is chosen randomly in this interval for each rapid fire.
* `excluded_sounds`: A list of IDs of soundboard sounds that the bot should never play.
* `use_default_sounds`: Whether or not to use Discord's default soundboard sounds in addition to the server's custom soundboard.
* `command_responses`: The text messages the bot will send to the voice channel's text chat to acknowledge it received a command. These can be left as default.
* `voice_model`: The exact filename of the `.onnx` Piper voice model file in your repository clone's root directory.
* `response_probability`: The probability that the bot will respond with a voice message when a user enters or leaves the voice channel, expressed as a number between 0 and 1.
* `min_voice_interval_seconds`/`max_voice_interval_seconds`: The minimum and maximum amount of seconds between the bot playing a random voice message. These messages are specified under `responses:random`.
* `responses`:
  * `joined`: A list of responses to choose from to play when a user joins the voice channel.
  * `left`: As above for when a user leaves the channel.
  * `random`: A list of messages to play at random intervals as specified by `min_voice_interval_seconds`/`max_voice_interval_seconds`.
