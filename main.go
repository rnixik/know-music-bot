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
	"time"
)

var genres map[string]string
var games map[string]*Game
var totalGames int64
var bot *tgbotapi.BotAPI
var db *sqlx.DB

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	db, err = sqlx.Open("mysql", os.Getenv("DB_USER")+":"+os.Getenv("DB_PASS")+"@tcp(localhost:3306)/"+os.Getenv("DB_NAME"))
	if err != nil {
		log.Panic(err)
	}

	proxyAddr := os.Getenv("PROXY_ADDR")

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

	games = make(map[string]*Game)

	if os.Getenv("BOT_DEBUG") == "true" {
		bot.Debug = true
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	startGarbageCollector()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {

		// Inline options
		if update.InlineQuery != nil {
			sendAnswerForInlineQuery(update.InlineQuery.ID)
			continue
		}

		// Clicks on buttons
		if update.CallbackQuery != nil {

			//log.Printf("CallbackQuery: %v", update.CallbackQuery)

			// Click on a button in private chat with bot
			if update.CallbackQuery.Message != nil && update.CallbackQuery.Message.Chat != nil && update.CallbackQuery.Data != "" {
				dataSlice := strings.Split(update.CallbackQuery.Data, ":")
				if len(dataSlice) != 3 {
					continue
				}

				gameInlineId := dataSlice[0]
				answerQuestionNumber, err := strconv.Atoi(dataSlice[1])
				if err != nil {
					log.Println(err)
					continue
				}
				answerMusicId, err := strconv.Atoi(dataSlice[2])
				if err != nil {
					log.Println(err)
					continue
				}
				game, ok := games[gameInlineId]
				if !ok {
					callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Game not found")
					bot.AnswerCallbackQuery(callbackConfig)
					continue
				}
				err = game.AnswerOnQuestion(update.CallbackQuery.ID, update.CallbackQuery.Message.Chat.ID, answerQuestionNumber, answerMusicId)
				if err != nil {
					log.Println(err)
					continue
				}
			}

			// CLick on a button in inline answer
			if update.CallbackQuery.Message == nil {

				inlineMessageId := update.CallbackQuery.InlineMessageID

				if strings.HasPrefix(update.CallbackQuery.Data, "play_") {

					genre := strings.Replace(update.CallbackQuery.Data, "play_", "", 1)
					log.Printf("%s: Start game with genre = %s", update.CallbackQuery.From.UserName, genre)

					game := NewGame(bot, db, inlineMessageId, genre)
					err := game.Start()
					if err != nil {
						log.Println(err)
						continue
					}

					games[inlineMessageId] = game
					totalGames += 1

				}

				continue
			}
		}

		// Direct messages
		if update.Message != nil && update.Message.Chat != nil {
			incomingText := update.Message.Text

			// Start shared game
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

			// Help
			if strings.HasPrefix(incomingText, "/start") || strings.HasPrefix(incomingText, "/help") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, "Use this bot in the inline mode. Type @"+bot.Self.UserName+" in any other chat and choose an option from the popup.")
				bot.Send(message)
				continue
			}

			// Top
			if strings.HasPrefix(incomingText, "/top") && isAdmin(update.Message.From.ID) {
				sendAdminTop(update.Message.Chat.ID)
				continue
			}

			// Audio upload
			if update.Message.Audio != nil {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Audio.FileID)
				message.BaseChat.ReplyToMessageID = update.Message.MessageID
				bot.Send(message)
				continue
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

func sendAnswerForInlineQuery(inlineQueryId string) {
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

func isAdmin(fromId int) bool {
	adminUserIdList := strings.Split(os.Getenv("ADMIN_USER_ID"), ",")
	for _, adminId := range adminUserIdList {
		if adminId == strconv.Itoa(fromId) {
			return true
		}
	}
	return true
}

func getMusicInfoText() (infoText string, err error) {
	type DbCountInfo struct {
		Field string `db:"f"`
		Count string `db:"cnt"`
	}

	type DbMusic struct {
		Title     string `db:"title"`
		Genre     string `db:"genre"`
		CreatedAt string `db:"created_at"`
	}

	infoText = ""

	totalRecords := []DbCountInfo{}
	err = db.Select(&totalRecords, "SELECT 'total' AS f, COUNT(*) AS cnt FROM music")
	if err != nil {
		return
	}
	infoText += "Total rows in DB: " + totalRecords[0].Count + "\n\n"

	countByGenre := []DbCountInfo{}
	err = db.Select(&countByGenre, "SELECT genre AS f, COUNT(*) AS cnt FROM music GROUP BY genre")
	if err != nil {
		return
	}

	infoText += "Number of rows in DB by genres:\n"
	for _, cntInfo := range countByGenre {
		infoText += cntInfo.Field + ": " + cntInfo.Count + "\n"
	}
	infoText += "\n"

	lastMusic := []DbMusic{}
	err = db.Select(&lastMusic, "SELECT title, genre, created_at FROM music ORDER BY created_at DESC LIMIT 1")
	if err != nil {
		return
	}
	infoText += "Last added music: " + lastMusic[0].Title + ", genre = " + lastMusic[0].Genre + ", added at " + lastMusic[0].CreatedAt + "\n\n"

	return
}

func sendAdminTop(chatId int64) {
	text := ""

	text += "Total number of games: " + strconv.FormatInt(totalGames, 10) + "\n\n"
	text += "Number of active games: " + strconv.Itoa(len(games)) + "\n\n"
	info, err := getMusicInfoText()
	if err != nil {
		text += "Error at DB: " + err.Error()
	}
	text += info

	gamesText := ""
	gameCounter := 0
	for _, game := range games {
		gameCounter += 1
		if gameCounter > 30 {
			break
		}
		gamesText += strconv.Itoa(gameCounter) + ". Started at " + game.startedAt.String() + ": \n"
		for _, player := range game.players {
			gamesText += player.Name + ": " + strconv.Itoa(player.Score) + "\n"
		}
		gamesText += "\n"
	}

	text += gamesText

	message := tgbotapi.NewMessage(chatId, text)
	bot.Send(message)
}

func startGarbageCollector() {
	ticker := time.NewTicker(10 * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				for gameKey, game := range games {
					if game.IsOld() {
						log.Println("Remove an old game")
						games[gameKey] = nil
						delete(games, gameKey)
					}
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}
