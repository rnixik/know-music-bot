package main

import (
  "log"
  "gopkg.in/telegram-bot-api.v4"
  "github.com/jmoiron/sqlx"
  _ "github.com/go-sql-driver/mysql"
  "math/rand"
  "strconv"
  "sort"
  "github.com/joho/godotenv"
  "os"
)

type Song struct {
  ID   int    `db:"id"`
  Title string `db:"title"`
  Artist string `db:"artist"`
  Lyrics string `db:"lyrics"`
  Lang string `db:"lang"`
  Genre string `db:"genre"`
  Options []Song
}

type UserScore struct {
  User *tgbotapi.User
  Score int
}

var rightAnswers map[int64]*Song
var usersScoresByChats map[int64](map[int]UserScore)

var genre string

func main() {

  err := godotenv.Load()
  if err != nil {
    log.Fatal("Error loading .env file")
  }

  conn, err := sqlx.Connect("mysql", os.Getenv("DB_USER") + ":" + os.Getenv("DB_PASS") + "@tcp(localhost:3306)/guess_song")
  if err != nil {
    panic(err)
  }

  bot, err := tgbotapi.NewBotAPI(os.Getenv("BOT_TOKEN"))
  if err != nil {
    log.Panic(err)
  }

  // chatId => songId
  rightAnswers = make(map[int64]*Song)
  usersScoresByChats = make(map[int64](map[int]UserScore))
  rightAnswerMessageIdByChannels := make(map[int64]int)
  activeQuestionMessageIdByChats := make(map[int64]int)

  genre = "alternative_rock"

  //bot.Debug = true


  log.Printf("Authorized on account %s", bot.Self.UserName)

  u := tgbotapi.NewUpdate(0)
  u.Timeout = 60

  updates, err := bot.GetUpdatesChan(u)

  for update := range updates {

    if update.CallbackQuery != nil {

      chatId := update.CallbackQuery.Message.Chat.ID

      activeQuestionMessageId, ok := activeQuestionMessageIdByChats[chatId]
      if !ok || activeQuestionMessageId != update.CallbackQuery.Message.MessageID {
        log.Printf("Answer is on expired message")
        continue
      }

      answerId, err := strconv.Atoi(update.CallbackQuery.Data)
      rightAnswerSong, ok := rightAnswers[chatId]; 
      if !ok || err != nil {
        continue
      }

      user := update.CallbackQuery.From

      if answerId == rightAnswerSong.ID {
        answerConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "You are right")
        bot.AnswerCallbackQuery(answerConfig)

        increaseScore(chatId, user)

        rightAnswerEditMessageId, ok := rightAnswerMessageIdByChannels[chatId]
        if ok {
            editMessageWithRightAnswer := makeEditMessageWithRightAnswer(chatId, rightAnswerEditMessageId, user, rightAnswerSong)
            bot.Send(editMessageWithRightAnswer)
        } else {
            messageWithRightAnswer := makeMessageWithRightAnswer(chatId, user, rightAnswerSong)
            sentMessage, _ := bot.Send(messageWithRightAnswer)
            rightAnswerMessageIdByChannels[chatId] = sentMessage.MessageID
        }

        song, err := getNextSong(conn)
        if err != nil {
          log.Panic(err)
          continue
        }

        onNewSong(chatId, &song)

        editMessageTextConfig, editMessageMarkupConfig := makeEditMessagesWithSong(chatId, update.CallbackQuery.Message.MessageID, song)
        bot.Send(editMessageTextConfig)
        bot.Send(editMessageMarkupConfig)
      } else {
        answerConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "That is the wrong answer")
        bot.AnswerCallbackQuery(answerConfig)

        decreaseScore(chatId, user)
      }

      continue
    }

    if update.Message == nil {
      continue
    }

    log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

    if update.Message.Text == "/play" || update.Message.Text == "/start" || update.Message.Text == "/play@GuessSongBot" {
      delete(rightAnswerMessageIdByChannels, update.Message.Chat.ID)

      song, err := getNextSong(conn)
      if err != nil {
        log.Panic(err)
        continue
      }

      onNewSong(update.Message.Chat.ID, &song)

      message := makeMessageWithSong(update.Message.Chat.ID, song)
      questionMessageSent, _ := bot.Send(message)
      activeQuestionMessageIdByChats[update.Message.Chat.ID] = questionMessageSent.MessageID
    }

    if update.Message.Text == "/top" || update.Message.Text == "/top@GuessSongBot" {
      chatId := update.Message.Chat.ID
      usersScores, ok := usersScoresByChats[chatId]
      if ok {
          message := tgbotapi.NewMessage(chatId, getTextWithHighScores(usersScores, update.Message.From))
          bot.Send(message)
      } else {
          message := tgbotapi.NewMessage(chatId, "No scores in this chat")
          bot.Send(message)
      }
    }
  }
}

func getNextSong(connect *sqlx.DB) (song Song, err error) {
  songs := []Song{}
  err = connect.Select(&songs, "SELECT * FROM songs WHERE lang = (SELECT lang FROM songs WHERE genre = ? ORDER BY RAND() LIMIT 1) AND genre = ? ORDER BY RAND() LIMIT 6", genre, genre)
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

func getKeyboardMarkUp(song Song) (keyboard tgbotapi.InlineKeyboardMarkup) {
    rows := [][]tgbotapi.InlineKeyboardButton{};
    for _, optionSong := range song.Options {
        button := tgbotapi.NewInlineKeyboardButtonData(optionSong.Artist + " - " + optionSong.Title, strconv.Itoa(optionSong.ID))
        row := tgbotapi.NewInlineKeyboardRow(button)
        rows = append(rows, row)
    }
    keyboard = tgbotapi.NewInlineKeyboardMarkup(rows...)
    return keyboard
}

func makeMessageWithSong(chatId int64, song Song) (message tgbotapi.MessageConfig) {
    message = tgbotapi.NewMessage(chatId, song.Lyrics)
    message.ReplyMarkup = getKeyboardMarkUp(song)
    return message
}

func makeEditMessagesWithSong(chatId int64, editMessageId int, song Song) (editMessage tgbotapi.EditMessageTextConfig, editMessageMarkup tgbotapi.EditMessageReplyMarkupConfig) {
    editMessage = tgbotapi.NewEditMessageText(chatId, editMessageId, song.Lyrics)
    editMessageMarkup = tgbotapi.NewEditMessageReplyMarkup(chatId, editMessageId, getKeyboardMarkUp(song))
    return
}

func onNewSong(chatId int64, song *Song) {
    rightAnswers[chatId] = song
}

func increaseScore(chatId int64, user *tgbotapi.User) {
    userScore := getUserScore(chatId, user)
    userScore.Score += 1
    usersScoresByChats[chatId][user.ID] = userScore
}

func decreaseScore(chatId int64, user *tgbotapi.User) {
    userScore := getUserScore(chatId, user)
    userScore.Score -= 1
    usersScoresByChats[chatId][user.ID] = userScore
}

func getUserScore(chatId int64, user *tgbotapi.User) UserScore {
    _, ok := usersScoresByChats[chatId]
    if !ok {
       usersScoresByChats[chatId] = make(map[int]UserScore)
    }
    userScore, okByUser := usersScoresByChats[chatId][user.ID]
    if !okByUser {
      userScore = UserScore{user, 0}
      usersScoresByChats[chatId][user.ID] = userScore
    }
    return userScore
}

func getMessageTextWithRightAnswer(chatId int64, user *tgbotapi.User, song *Song) string {
  userScore := getUserScore(chatId, user)
  return "The right answer is *" + song.Artist + " - " + song.Title + "*\nThe first was *" + user.String() + "* (" + strconv.Itoa(userScore.Score) + ")"
}

func makeMessageWithRightAnswer(chatId int64, user *tgbotapi.User, song *Song) (message tgbotapi.MessageConfig) {
  message = tgbotapi.NewMessage(chatId, getMessageTextWithRightAnswer(chatId, user, song))
  message.ParseMode = "markdown"
  return message
}

func makeEditMessageWithRightAnswer(chatId int64, editMessageId int, user *tgbotapi.User, song *Song) (editMessage tgbotapi.EditMessageTextConfig) {
  editMessage = tgbotapi.NewEditMessageText(chatId, editMessageId, getMessageTextWithRightAnswer(chatId, user, song))
  editMessage.ParseMode = "markdown"
  return editMessage
}

func getTextWithHighScores(usersScores map[int]UserScore, user *tgbotapi.User) string {
  scorePairs := orderByScoresDesc(usersScores)
  text := "Top:\n"
  for i, scorePair := range scorePairs {
    text += strconv.Itoa(i + 1) + ". " + scorePair.Value.User.String() + ": " + strconv.Itoa(scorePair.Value.Score) + "\n"
    if (i > 10) {
      break
    }
  }
  return text
}

// sort highscores
func orderByScoresDesc(usersScores map[int]UserScore) ScorePairList{
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
  Key int
  Value UserScore
}

type ScorePairList []ScorePair

func (p ScorePairList) Len() int { return len(p) }
func (p ScorePairList) Less(i, j int) bool { return p[i].Value.Score < p[j].Value.Score }
func (p ScorePairList) Swap(i, j int){ p[i], p[j] = p[j], p[i] }
// end of sort
