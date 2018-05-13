package main

import (
	"fmt"
	"github.com/go-ini/ini"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"golang.org/x/net/proxy"
	"gopkg.in/telegram-bot-api.v4"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Sound struct {
	ID      int    `db:"id"`
	Title   string `db:"title"`
	FileID  string `db:"file_id"`
	Genre   string `db:"genre"`
	Options []Sound
}

type DbCountInfo struct {
	Field string `db:"f"`
	Count string `db:"cnt"`
}

type UserScore struct {
	User  *tgbotapi.User
	Score int
}

const (
	defaultGenre = "any"
	anyGenre     = "any"
	maxScore     = 7
)

var rightAnswersByInlineMessages map[string]*Sound
var usersScoresByInlineMessages map[string](map[int]UserScore)
var genresByInlineMessages map[string]string

var genres map[string]string

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

	rightAnswersByInlineMessages = make(map[string]*Sound)
	usersScoresByInlineMessages = make(map[string](map[int]UserScore))
	genresByInlineMessages = make(map[string]string)

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
		}

		if update.CallbackQuery != nil {

			log.Printf("CallbackQuery: %v", update.CallbackQuery)

			if update.CallbackQuery.Message == nil {

				inlineMessageId := update.CallbackQuery.InlineMessageID

				if strings.HasPrefix(update.CallbackQuery.Data, "play_") {

					genre := strings.Replace(update.CallbackQuery.Data, "play_", "", 1)
					log.Printf("%s: Start game with genre = %s", update.CallbackQuery.From.UserName, genre)
					genresByInlineMessages[inlineMessageId] = genre

					err := sendNextQuestion(conn, bot, inlineMessageId, "")
					if err != nil {
						log.Println(err)
						continue
					}

				} else {

					userAnswerSoundId, err := strconv.Atoi(update.CallbackQuery.Data)
					if err != nil || userAnswerSoundId == 0 {
						log.Printf("Can't parse user's answer")
						continue
					}

					rightAnswerSound, ok := rightAnswersByInlineMessages[inlineMessageId]
					if !ok {
						editOutdatedConfig := tgbotapi.EditMessageTextConfig{
							BaseEdit: tgbotapi.BaseEdit{
								InlineMessageID: inlineMessageId,
							},
							Text: "Sorry, the bot was restarted. Please, start a new game.",
						}
						bot.Send(editOutdatedConfig)
						continue
					}

					user := update.CallbackQuery.From

					var responseToUserText string
					if rightAnswerSound.ID == userAnswerSoundId {
						responseToUserText = "You are right!"
						increaseScore(inlineMessageId, user)
					} else {
						responseToUserText = "That is the wrong answer"
						decreaseScore(inlineMessageId, user)
					}
					callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, responseToUserText)
					bot.AnswerCallbackQuery(callbackConfig)

					if rightAnswerSound.ID == userAnswerSoundId {
						hasWinner := checkWinner(bot, inlineMessageId, user)
						if hasWinner {
							continue
						}
					}

					if rightAnswerSound.ID == userAnswerSoundId {
						prefixText := "The right answer was\n*" + rightAnswerSound.Title + "*.\n"
						prefixText += "*" + getUserFullName(user) + "* was the first!\n\n"

						usersScores, ok := usersScoresByInlineMessages[inlineMessageId]
						if ok {
							prefixText += "Current Top (limit " + strconv.Itoa(maxScore) + "):\n" + getTextWithHighScores(usersScores) + "\n"
						}

						prefixText += "Ok. Now the next question.\n"

						err = sendNextQuestion(conn, bot, inlineMessageId, prefixText)
						if err != nil {
							log.Println(err)
							continue
						}
					}
				}

				continue
			}
		}

		if update.Message != nil && update.Message.Chat != nil {
			incomingText := update.Message.Text

			if strings.HasPrefix(incomingText, "/start ") {
				inlineMessageId := strings.Replace(incomingText, "/start ", "", 1)
				rightAnswerSound, ok := rightAnswersByInlineMessages[inlineMessageId]
				if ok {
					audio := tgbotapi.NewAudioShare(update.Message.Chat.ID, rightAnswerSound.FileID)
					bot.Send(audio)
					continue
				}
			}

			if strings.HasPrefix(incomingText, "/start") || strings.HasPrefix(incomingText, "/help") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, "Use this bot in the inline mode. Type @KnowMusicBot in any other chat and choose an option from the popup.")
				bot.Send(message)
				continue
			}

			if strings.HasPrefix(incomingText, "/top") && strconv.Itoa(update.Message.From.ID) == adminUserId {
				sendTop(bot, update.Message.Chat.ID)
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

func sendNextQuestion(connect *sqlx.DB, bot *tgbotapi.BotAPI, inlineMessageId string, prefixText string) error {
	genre, ok := genresByInlineMessages[inlineMessageId]
	if !ok {
		genre = defaultGenre
	}

	sound, err := getNextSound(connect, genre)
	if err != nil {
		fmt.Printf("Can't get next sound for genre = %s", genre)
		return err
	}
	rightAnswersByInlineMessages[inlineMessageId] = sound

	text := prefixText
	text = text + getQuestionTextWithSound(sound, inlineMessageId)
	keyboardMarkup := getKeyboardMarkup(sound)

	editConfig := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			InlineMessageID: inlineMessageId,
			ReplyMarkup:     &keyboardMarkup,
		},
		Text:      text,
		ParseMode: "markdown",
	}
	_, err = bot.Send(editConfig)

	return err
}

func getNextSound(connect *sqlx.DB, genre string) (sound *Sound, err error) {
	sounds := []Sound{}

	if genre == anyGenre {
		err = connect.Select(&sounds, "SELECT sounds.* FROM sounds ORDER BY RAND() LIMIT 5")
	} else {
		err = connect.Select(&sounds, "SELECT sounds.* FROM sounds WHERE genre = ? ORDER BY RAND() LIMIT 5", genre)
	}
	if err != nil {
		return
	}

	if len(sounds) == 0 {
		return nil, fmt.Errorf("Sounds not found by genre = %s", genre)
	}

	randInd := rand.Intn(len(sounds))

	sound = &sounds[randInd]
	sound.Options = sounds

	return sound, nil
}

func getKeyboardMarkup(sound *Sound) (keyboard tgbotapi.InlineKeyboardMarkup) {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, optionSound := range sound.Options {
		button := tgbotapi.NewInlineKeyboardButtonData(optionSound.Title, strconv.Itoa(optionSound.ID))
		row := tgbotapi.NewInlineKeyboardRow(button)
		rows = append(rows, row)
	}
	keyboard = tgbotapi.NewInlineKeyboardMarkup(rows...)
	return keyboard
}

func getQuestionTextWithSound(sound *Sound, inlineMessageId string) string {
	text := "_Where does_ [this music](https://t.me/KnowMusicBot?start=" + inlineMessageId + ") _play?_\n\n"
	return text
}

func increaseScore(inlineMessageId string, user *tgbotapi.User) {
	userScore := getUserScore(inlineMessageId, user)
	userScore.Score += 1
	usersScoresByInlineMessages[inlineMessageId][user.ID] = userScore
}

func decreaseScore(inlineMessageId string, user *tgbotapi.User) {
	userScore := getUserScore(inlineMessageId, user)
	userScore.Score -= 1
	usersScoresByInlineMessages[inlineMessageId][user.ID] = userScore
}

func checkWinner(bot *tgbotapi.BotAPI, inlineMessageId string, user *tgbotapi.User) bool {
	userScore := getUserScore(inlineMessageId, user)
	if userScore.Score >= maxScore {
		text := "We have a winner - *" + getUserFullName(user) + "*!\nHe's got " + strconv.Itoa(userScore.Score) + " points.\n\n"

		usersScores, ok := usersScoresByInlineMessages[inlineMessageId]
		if ok {
			text += "Current Top:\n" + getTextWithHighScores(usersScores) + "\n"
		}
		text += "\nThis game is over."

		editConfig := tgbotapi.EditMessageTextConfig{
			BaseEdit: tgbotapi.BaseEdit{
				InlineMessageID: inlineMessageId,
			},
			Text:      text,
			ParseMode: "markdown",
		}

		bot.Send(editConfig)
		return true
	}
	return false
}

func getUserScore(inlineMessageId string, user *tgbotapi.User) UserScore {
	_, ok := usersScoresByInlineMessages[inlineMessageId]
	if !ok {
		usersScoresByInlineMessages[inlineMessageId] = make(map[int]UserScore)
	}
	userScore, okByUser := usersScoresByInlineMessages[inlineMessageId][user.ID]
	if !okByUser {
		userScore = UserScore{user, 0}
		usersScoresByInlineMessages[inlineMessageId][user.ID] = userScore
	}
	return userScore
}

func getTextWithHighScores(usersScores map[int]UserScore) string {
	scorePairs := orderByScoresDesc(usersScores)
	text := ""
	for i, scorePair := range scorePairs {
		text += strconv.Itoa(i+1) + ". " + getUserFullName(scorePair.Value.User) + ": " + strconv.Itoa(scorePair.Value.Score) + "\n"
		if i > 10 {
			break
		}
	}
	return text
}

// sort highscores
func orderByScoresDesc(usersScores map[int]UserScore) ScorePairList {
	pl := make(ScorePairList, len(usersScores))
	i := 0
	for k, v := range usersScores {
		pl[i] = ScorePair{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

type ScorePair struct {
	Key   int
	Value UserScore
}

type ScorePairList []ScorePair

func (p ScorePairList) Len() int           { return len(p) }
func (p ScorePairList) Less(i, j int) bool { return p[i].Value.Score < p[j].Value.Score }
func (p ScorePairList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// end of sort

func sendTop(bot *tgbotapi.BotAPI, chatId int64) {
	text := ""
	for _, usersScore := range usersScoresByInlineMessages {
		text += getTextWithHighScores(usersScore) + "\n\n"
	}
	if text == "" {
		text = "No top"
	}
	message := tgbotapi.NewMessage(chatId, text)
	bot.Send(message)
}
