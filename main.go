package main

import (
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"gopkg.in/telegram-bot-api.v4"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Song struct {
	ID      int    `db:"id"`
	Title   string `db:"title"`
	Artist  string `db:"artist"`
	Lyrics  string `db:"lyrics"`
	Lang    string `db:"lang"`
	Genre   string `db:"genre"`
	Options []Song
}

type UserScore struct {
	User  *tgbotapi.User
	Score int
}

const (
	defaultGenre = "alternative_rock"
)

var rightAnswersByInlineMessages map[string]*Song
var usersScoresByInlineMessages map[string](map[int]UserScore)
var genresByInlineMessages map[string]string

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	conn, err := sqlx.Open("mysql", os.Getenv("DB_USER")+":"+os.Getenv("DB_PASS")+"@tcp(localhost:3306)/guess_song")
	if err != nil {
		log.Panic(err)
	}

	bot, err := tgbotapi.NewBotAPI(os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	rightAnswersByInlineMessages = make(map[string]*Song)
	usersScoresByInlineMessages = make(map[string](map[int]UserScore))
	genresByInlineMessages = make(map[string]string)

	//bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {

		if update.InlineQuery != nil {
			sendAnswerForInlineQuery(bot, update.InlineQuery.ID)
		}

		if update.CallbackQuery != nil {

			//log.Printf("CallbackQuery: %v", update.CallbackQuery)

			if update.CallbackQuery.Message == nil {

				inlineMessageId := update.CallbackQuery.InlineMessageID

				if strings.HasPrefix(update.CallbackQuery.Data, "play_") {

					genre := strings.Replace(update.CallbackQuery.Data, "play_", "", 1)
					log.Printf("Start game with genre = %s", genre)
					genresByInlineMessages[inlineMessageId] = genre

					err := sendNextQuestion(conn, bot, inlineMessageId, "")
					if err != nil {
						log.Panic(err)
						continue
					}

				} else {

					userAnswerSongId, err := strconv.Atoi(update.CallbackQuery.Data)
					if err != nil || userAnswerSongId == 0 {
						log.Printf("Can't parse user's answer")
						continue
					}

					rightAnswerSong, ok := rightAnswersByInlineMessages[inlineMessageId]
					if !ok {
						log.Printf("Can't get the right answer")
						continue
					}

					user := update.CallbackQuery.From

					var responseToUserText string
					if rightAnswerSong.ID == userAnswerSongId {
						responseToUserText = "You are right!"
						increaseScore(inlineMessageId, user)
					} else {
						responseToUserText = "That is the wrong answer"
						decreaseScore(inlineMessageId, user)
					}
					callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, responseToUserText)
					bot.AnswerCallbackQuery(callbackConfig)

					if rightAnswerSong.ID == userAnswerSongId {
						prefixText := "The right answer was\n*" + rightAnswerSong.Artist + " - " + rightAnswerSong.Title + "*.\n"
						prefixText += "*" + getUserFullName(user) + "* was the first!\n\n"

						usersScores, ok := usersScoresByInlineMessages[inlineMessageId]
						if ok {
							prefixText += getTextWithHighScores(usersScores) + "\n"
						}

						prefixText += "Ok. Now the next question.\n"

						err = sendNextQuestion(conn, bot, inlineMessageId, prefixText)
						if err != nil {
							log.Panic(err)
							continue
						}
					}
				}

				continue
			}
		}

		if update.Message != nil && update.Message.Chat != nil {
			incomingText := update.Message.Text
			if strings.HasPrefix(incomingText, "/start") || strings.HasPrefix(incomingText, "/help") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, "Use this bot in the inline mode. Type @GuessSongBot in any other chat and choose an option from the popup.")
				bot.Send(message)
			}
		}
	}
}

func sendAnswerForInlineQuery(bot *tgbotapi.BotAPI, inlineQueryId string) {
	description := "Bot shows lyrics from a random song and provides 5 options of titles to answer. The first player who answers right gets +1 point. If player's answer is wrong, he gets -1 point."
	inlineResult := tgbotapi.NewInlineQueryResultArticle("alternative_rock", "Play the game with Alternative Rock", description+" Genre = Alternative Rock (not only).")
	keyboard := tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Start", "play_alternative_rock")})
	inlineResult.ReplyMarkup = &keyboard

	inlineResult2 := tgbotapi.NewInlineQueryResultArticle("russian_pop", "Play the game with Russian Pop", description+" Genre = Russian Pop (not only).")
	keyboard2 := tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData("Start", "play_russian_pop")})
	inlineResult2.ReplyMarkup = &keyboard2

	inlineResults := []tgbotapi.InlineQueryResultArticle{inlineResult, inlineResult2}

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

	song, err := getNextSong(connect, genre)
	if err != nil {
		return err
	}
	rightAnswersByInlineMessages[inlineMessageId] = &song

	text := prefixText
	text = text + getQuestionTextWithSong(&song)
	keyboardMarkup := getKeyboardMarkup(&song)

	editConfig := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			InlineMessageID: inlineMessageId,
			ReplyMarkup:     &keyboardMarkup,
		},
		Text:      text,
		ParseMode: "markdown",
	}

	bot.Send(editConfig)
	return nil
}

func getNextSong(connect *sqlx.DB, genre string) (song Song, err error) {
	songs := []Song{}
	err = connect.Select(&songs, "SELECT songs.* FROM songs INNER JOIN (SELECT lang, genre FROM songs WHERE genre = ? ORDER BY RAND() LIMIT 1) AS fs ON fs.lang = songs.lang AND songs.genre = fs.genre ORDER BY RAND() LIMIT 6", genre)
	if err != nil {
		return
	}

	song = songs[0]
	song.Options = songs[0:5]

	// randomize
	for i := range song.Options {
		j := rand.Intn(i + 1)
		song.Options[i], song.Options[j] = song.Options[j], song.Options[i]
	}

	return song, nil
}

func getKeyboardMarkup(song *Song) (keyboard tgbotapi.InlineKeyboardMarkup) {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, optionSong := range song.Options {
		button := tgbotapi.NewInlineKeyboardButtonData(optionSong.Artist+" - "+optionSong.Title, strconv.Itoa(optionSong.ID))
		row := tgbotapi.NewInlineKeyboardRow(button)
		rows = append(rows, row)
	}
	keyboard = tgbotapi.NewInlineKeyboardMarkup(rows...)
	return keyboard
}

func getQuestionTextWithSong(song *Song) string {
	text := "_Who sings the following text?_\n\n" + song.Lyrics
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
	text := "Current Top:\n"
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
