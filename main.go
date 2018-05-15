package main

import (
	"github.com/go-ini/ini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"golang.org/x/net/proxy"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var genres map[string]string
var games map[string]*Game

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	conn, err := sqlx.Open("mysql", os.Getenv("DB_USER")+":"+os.Getenv("DB_PASS")+"@tcp(localhost:3306)/"+os.Getenv("DB_NAME"))
	if err != nil {
		log.Panic(err)
	}

	proxyAddr := os.Getenv("PROXY_ADDR")

	var bot *tgbotapi.BotAPI

	if len(proxyAddr) > 0 {
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			log.Panic(err)
		}
		httpTransport := &http.Transport{}
		httpClient := &http.Client{Transport: httpTransport}
		httpTransport.Dial = dialer.Dial

		bot, err = tgbotapi.NewBotAPIWithClient(os.Getenv("BOT_TOKEN"), httpClient)
		if err != nil {
			log.Panic(err)
		}
	} else {
		bot, err = tgbotapi.NewBotAPI(os.Getenv("BOT_TOKEN"))
		if err != nil {
			log.Panic(err)
		}
	}

	genresIni, err := ini.Load("genres.ini")
	if err != nil {
		log.Fatal("Error loading genres.ini file")
	}
	genres = genresIni.Section("").KeysHash()

	adminUserId := os.Getenv("ADMIN_USER_ID")

	games := make(map[string]*Game)

	if os.Getenv("BOT_DEBUG") == "true" {
		bot.Debug = true
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {

		if update.InlineQuery != nil {
			sendAnswerForInlineQuery(bot, update.InlineQuery.ID)
			continue
		}

		if update.CallbackQuery != nil {

			//log.Printf("CallbackQuery: %v", update.CallbackQuery)

			if update.CallbackQuery.Message != nil && update.CallbackQuery.Message.Chat != nil && update.CallbackQuery.Data != "" {
				dataSlice := strings.Split(update.CallbackQuery.Data, ":")
				if len(dataSlice) != 3 {
					continue
				}

				gameInlineId := dataSlice[0]
				answerQuestionNumber, err := strconv.Atoi(dataSlice[1])
				if err != nil {
					continue
				}
				answerMusicId, err := strconv.Atoi(dataSlice[2])
				if err != nil {
					continue
				}
				game, ok := games[gameInlineId]
				if !ok {
					continue
				}
				game.AnswerOnQuestion(update.CallbackQuery.ID, update.CallbackQuery.Message.Chat.ID, answerQuestionNumber, answerMusicId)
			}

			if update.CallbackQuery.Message == nil {

				inlineMessageId := update.CallbackQuery.InlineMessageID

				if strings.HasPrefix(update.CallbackQuery.Data, "play_") {

					genre := strings.Replace(update.CallbackQuery.Data, "play_", "", 1)
					log.Printf("%s: Start game with genre = %s", update.CallbackQuery.From.UserName, genre)

					game := NewGame(bot, conn, inlineMessageId, genre)
					err := game.Start()
					if err != nil {
						log.Println(err)
						continue
					}

					games[inlineMessageId] = game

				}

				continue
			}
		}

		if update.Message != nil && update.Message.Chat != nil {
			incomingText := update.Message.Text

			if strings.HasPrefix(incomingText, "/start ") {
				inlineMessageId := strings.Replace(incomingText, "/start ", "", 1)
				game, ok := games[inlineMessageId]
				if ok {
					game.JoinPlayer(update.Message.Chat.ID, getUserFullName(update.Message.From))
				} else {
					message := tgbotapi.NewMessage(update.Message.Chat.ID, "Your game not found. To start the game use this bot in the inline mode. Type @KnowMusicBot in any other chat and choose an option from the popup. Then follow the link.")
					bot.Send(message)
				}
				continue
			}

			if strings.HasPrefix(incomingText, "/start") || strings.HasPrefix(incomingText, "/help") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, "Use this bot in the inline mode. Type @KnowMusicBot in any other chat and choose an option from the popup.")
				bot.Send(message)
				continue
			}

			if strings.HasPrefix(incomingText, "/top") && strconv.Itoa(update.Message.From.ID) == adminUserId {
				continue
			}

			if update.Message.Audio != nil {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Audio.FileID)
				message.BaseChat.ReplyToMessageID = update.Message.MessageID
				bot.Send(message)
			}
		}
	}
}

func getUserFullName(u *tgbotapi.User) string {
	name := u.FirstName
	if u.LastName != "" {
		name += " " + u.LastName
	}

	if name == "" && u.UserName != "" {
		return u.UserName
	}

	return name
}

func sendAnswerForInlineQuery(bot *tgbotapi.BotAPI, inlineQueryId string) {
	description := "Bot sends part of soundtrack from a random game or film and provides 5 options of titles to answer. The first player who answers right gets +1 point. If player's answer is wrong, he gets -1 point. The first player with " + strconv.Itoa(maxScore) + " points is the winner."

	inlineResults := []tgbotapi.InlineQueryResultArticle{}
	for genreKey, genreDescription := range genres {
		inlineResult := tgbotapi.NewInlineQueryResultArticle(genreKey, "Play the game with "+genreDescription, description+" Genre = "+genreDescription+".")
		keyboard := tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Start", "play_"+genreKey)})
		inlineResult.ReplyMarkup = &keyboard
		inlineResults = append(inlineResults, inlineResult)
	}

	// convert to slice of interfaces
	inlineResultsInterfaces := make([]interface{}, len(inlineResults))
	for i, v := range inlineResults {
		inlineResultsInterfaces[i] = v
	}

	inlineConfig := tgbotapi.InlineConfig{}
	inlineConfig.InlineQueryID = inlineQueryId
	inlineConfig.Results = inlineResultsInterfaces
	bot.AnswerInlineQuery(inlineConfig)
}
