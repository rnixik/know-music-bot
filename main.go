package main

import (
	"fmt"
	"github.com/go-ini/ini"
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
	ID          int    `db:"id"`
	Title       string `db:"title"`
	Artist      string `db:"artist"`
	Lyrics      string `db:"lyrics"`
	Lang        string `db:"lang"`
	Genre       string `db:"genre"`
	SourceGenre string `db:"source_genre"`
	Options     []Song
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
	defaultGenre = "alternative_rock"
	anyGenre     = "any"
)

var rightAnswersByInlineMessages map[string]*Song
var usersScoresByInlineMessages map[string](map[int]UserScore)
var genresByInlineMessages map[string]string

var genres map[string]string

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

	genresIni, err := ini.Load("genres.ini")
	if err != nil {
		log.Fatal("Error loading genres.ini file")
	}
	genres = genresIni.Section("").KeysHash()

	adminUserId := os.Getenv("ADMIN_USER_ID")

	rightAnswersByInlineMessages = make(map[string]*Song)
	usersScoresByInlineMessages = make(map[string](map[int]UserScore))
	genresByInlineMessages = make(map[string]string)

	infoText, errInfo := getSongsInfo(conn)
	if errInfo != nil {
		log.Printf("Can't get songs info: %v", errInfo)
	}

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
					log.Printf("%s: Start game with genre = %s", update.CallbackQuery.From.UserName, genre)
					genresByInlineMessages[inlineMessageId] = genre

					err := sendNextQuestion(conn, bot, inlineMessageId, "")
					if err != nil {
						log.Println(err)
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
							prefixText += "Current Top:\n" + getTextWithHighScores(usersScores) + "\n"
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
			if strings.HasPrefix(incomingText, "/start") || strings.HasPrefix(incomingText, "/help") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, "Use this bot in the inline mode. Type @GuessSongBot in any other chat and choose an option from the popup.")
				bot.Send(message)
				continue
			}

			if strings.HasPrefix(incomingText, "/top") && strconv.Itoa(update.Message.From.ID) == adminUserId {
				sendTop(bot, update.Message.Chat.ID)
				continue
			}

			if strings.HasPrefix(incomingText, "/info") {
				message := tgbotapi.NewMessage(update.Message.Chat.ID, infoText)
				bot.Send(message)
				continue
			}

		}
	}
}

func sendAnswerForInlineQuery(bot *tgbotapi.BotAPI, inlineQueryId string) {
	description := "Bot shows lyrics from a random song and provides 5 options of titles to answer. The first player who answers right gets +1 point. If player's answer is wrong, he gets -1 point. Playlist contains top 100 performers from YM by genre."

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

	song, err := getNextSong(connect, genre)
	if err != nil {
		fmt.Printf("Can't get next song for genre = %s", genre)
		return err
	}
	rightAnswersByInlineMessages[inlineMessageId] = song

	text := prefixText
	text = text + getQuestionTextWithSong(song)
	keyboardMarkup := getKeyboardMarkup(song)

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

func getNextSong(connect *sqlx.DB, genre string) (song *Song, err error) {
	songs := []Song{}

	if genre == anyGenre {
		err = connect.Select(&songs, "SELECT songs.* FROM songs INNER JOIN (SELECT lang FROM songs ORDER BY RAND() LIMIT 1) AS fs ON fs.lang = songs.lang ORDER BY RAND() LIMIT 5")
	} else {
		err = connect.Select(&songs, "SELECT songs.* FROM songs INNER JOIN (SELECT lang, source_genre FROM songs WHERE source_genre = ? ORDER BY RAND() LIMIT 1) AS fs ON fs.lang = songs.lang AND songs.source_genre = fs.source_genre ORDER BY RAND() LIMIT 5", genre)
	}
	if err != nil {
		return
	}

	if len(songs) == 0 {
		return nil, fmt.Errorf("Songs not found by genre = %s", genre)
	}

	rainInd := rand.Intn(len(songs))

	song = &songs[rainInd]
	song.Options = songs

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

func getSongsInfo(connect *sqlx.DB) (infoText string, err error) {
	genreTracks := []DbCountInfo{}
	err = connect.Select(&genreTracks, "SELECT source_genre AS f, COUNT(1) AS cnt FROM songs GROUP BY source_genre")
	genreArtists := []DbCountInfo{}
	err = connect.Select(&genreArtists, "SELECT source_genre AS f, COUNT(DISTINCT artist) AS cnt FROM songs GROUP BY source_genre")
	infoText = "Tracks by genre:\n"
	for _, cntInfoTracks := range genreTracks {
		infoText += cntInfoTracks.Field + " - " + cntInfoTracks.Count + "\n"
	}

	infoText += "\nArtists by genre:\n"
	for _, cntInfoTracks := range genreArtists {
		infoText += cntInfoTracks.Field + " - " + cntInfoTracks.Count + "\n"
	}
	return
}
