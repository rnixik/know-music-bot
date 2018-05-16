# KnowMusicBot - a bot for Telegram Messanger

[Bot](https://t.me/KnowMusicBot) sends short part of random music and provides 5 options of titles to answer.
The first player who answers right gets +1 point. If player's answer is wrong, he gets -1 point.

Bot works in inline mode. Any user in any chat can start the game typing `@KnowMusicBot `.

## Run

1. Create a new MySQL database using `schema.sql`
2. Fill table `sounds` with some titles
3. Create a new bot using [@BotFather](https://t.me/BotFather). Save the token
4. Copy `.env.example` to `.env`
5. Open and edit `.env`. Provide mysql credentials and token of your bot.
6. Run `go get ./... && go build && ./know-music-bot`

## License

The MIT License
