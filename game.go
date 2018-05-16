package main

import (
	"fmt"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/jmoiron/sqlx"
	"math/rand"
	"sort"
	"strconv"
	"time"
)

const (
	defaultGenre = "any"
	anyGenre     = "any"
	maxScore     = 5
)

type Player struct {
	chatID                int64
	Name                  string
	Score                 int
	lastQuestionMessageID int
}

type Music struct {
	ID      int    `db:"id"`
	Title   string `db:"title"`
	FileID  string `db:"file_id"`
	Genre   string `db:"genre"`
	Lang    string `db:"lang"`
	Options []Music
}

type Game struct {
	bot              *tgbotapi.BotAPI
	db               *sqlx.DB
	inlineMessageId  string
	players          map[int64]*Player
	lastJoinedPlayer *Player
	genre            string
	startedAt        time.Time
	currentMusic     *Music
	questionNumber   int
	scoreLimit       int
	prevMusic        *Music
	prevRightPlayer  *Player
	winner           *Player
}

func NewGame(bot *tgbotapi.BotAPI, db *sqlx.DB, inlineMessageId string, genre string) *Game {
	return &Game{
		bot:             bot,
		db:              db,
		inlineMessageId: inlineMessageId,
		players:         make(map[int64]*Player),
		genre:           genre,
		startedAt:       time.Now(),
		scoreLimit:      maxScore,
	}
}

func newPlayer(chatID int64, name string) *Player {
	return &Player{
		chatID: chatID,
		Name:   name,
		Score:  0,
	}
}

func (g *Game) Start() (err error) {
	return g.changeCurrentQuestionWithBroadcast()
}

func (g *Game) JoinPlayer(chatID int64, name string) (err error) {
	g.players[chatID] = newPlayer(chatID, name)
	g.lastJoinedPlayer = g.players[chatID]
	err = g.sendQuestion(g.players[chatID])
	if err != nil {
		return err
	}
	return g.updateInvitationMessage()
}

func (g *Game) AnswerOnQuestion(callbackQueryID string, chatID int64, answerQuestionNumber int, answerMusicID int) (err error) {
	player, ok := g.players[chatID]
	if !ok {
		return fmt.Errorf("Player not found in game by chat id = %d", chatID)
	}
	var responseToUserText string
	if g.questionNumber != answerQuestionNumber {
		responseToUserText = "You answer is outdated!"
	} else if g.currentMusic.ID == answerMusicID {
		responseToUserText = "You are right!"
	} else {
		responseToUserText = "That is the wrong answer"
	}

	callbackConfig := tgbotapi.NewCallback(callbackQueryID, responseToUserText)
	g.bot.AnswerCallbackQuery(callbackConfig)

	if g.questionNumber != answerQuestionNumber {
		return nil
	}

	if g.currentMusic.ID == answerMusicID {
		player.Score += 1
		g.prevRightPlayer = player
		g.prevMusic = g.currentMusic
		hasWinner := g.hasWinner(player)
		if hasWinner {
			g.winner = player
			g.end()
			return nil
		}
	} else {
		player.Score -= 1
	}

	if g.currentMusic.ID == answerMusicID {
		err = g.changeCurrentQuestionWithBroadcast()
	}

	return err
}

func (g *Game) IsEnded() bool {
	return g.winner != nil
}

func (g *Game) hasWinner(player *Player) bool {
	return player.Score >= g.scoreLimit
}

func (g *Game) end() {
	for _, player := range g.players {
		g.deleteLastQuestion(player)
		g.sendStatus(player)
	}
	g.updateInvitationMessage()
}

func (g *Game) changeCurrentQuestionWithBroadcast() (err error) {
	music, err := g.getRandomMusic()
	if err != nil {
		return err
	}
	g.currentMusic = music
	g.questionNumber = g.questionNumber + 1
	g.broadcastQuestion()
	return g.updateInvitationMessage()
}

func (g *Game) sendQuestion(player *Player) (err error) {
	g.deleteLastQuestion(player)
	g.sendStatus(player)
	audio := tgbotapi.NewAudioShare(player.chatID, g.currentMusic.FileID)
	keyboardMarkup := g.getKeyboardMarkupForCurrentMusic()
	audio.BaseFile.BaseChat.ReplyMarkup = keyboardMarkup
	result, err := g.bot.Send(audio)
	if err != nil {
		return err
	}
	player.lastQuestionMessageID = result.MessageID
	return nil
}

func (g *Game) deleteLastQuestion(player *Player) (err error) {
	if player.lastQuestionMessageID == 0 {
		return nil
	}
	deleteMessage := tgbotapi.NewDeleteMessage(player.chatID, player.lastQuestionMessageID)
	_, err = g.bot.Send(deleteMessage)
	player.lastQuestionMessageID = 0
	return err
}

func (g *Game) sendStatus(player *Player) (err error) {
	text := g.getStatusText()
	message := tgbotapi.NewMessage(player.chatID, text)
	_, err = g.bot.Send(message)
	return err
}

func (g *Game) broadcastQuestion() {
	for _, player := range g.players {
		g.sendQuestion(player)
	}
}

func (g *Game) getStatusText() string {
	text := ""

	if g.IsEnded() {
		text += "Game has been ended!\n"
		text += "-->>Winner is *" + g.winner.Name + "*<<--\n\n"
	}

	if len(g.players) > 0 {
		text += "Number of players in this session: " + strconv.Itoa(len(g.players)) + ". Last joined is _" + g.lastJoinedPlayer.Name + "_.\n\n"
	}

	if g.prevMusic != nil && g.prevRightPlayer != nil {
		text += "The right answer was\n*" + g.prevMusic.Title + "*.\n"
		text += "*" + g.prevRightPlayer.Name + "* was the first!\n\n"
	}

	if len(g.players) > 0 {
		text += "Current Top (limit " + strconv.Itoa(g.scoreLimit) + "):\n" + g.getTextWithHighScores() + "\n"
	}

	if g.currentMusic != nil && !g.IsEnded() {
		text += "Question number: " + strconv.Itoa(g.questionNumber) + "\n"
	}

	text += "\n\n"
	return text
}

func (g *Game) updateInvitationMessage() (err error) {
	text := ""
	if !g.IsEnded() {
		text += "We have started a new game with genre = " + g.genre + ". "
		text += "Guess title of music by a short extraction of sound.\n"
		text += "[-->>Join the game<<--](https://t.me/" + g.bot.Self.UserName + "?start=" + g.inlineMessageId + ")\n\n"
	}

	text += g.getStatusText()

	editConfig := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			InlineMessageID: g.inlineMessageId,
		},
		Text:      text,
		ParseMode: "markdown",
	}
	_, err = g.bot.Send(editConfig)
	return err
}

func (g *Game) getRandomMusic() (m *Music, err error) {
	music := []Music{}

	err = g.db.Select(&music, "SELECT id, title, file_id, genre, lang FROM music WHERE (genre = ? OR '"+anyGenre+"' = ?) ORDER BY RAND() LIMIT 5", g.genre, g.genre)

	if err != nil {
		return
	}

	if len(music) == 0 {
		return nil, fmt.Errorf("Music not found by genre = %s", g.genre)
	}

	randInd := rand.Intn(len(music))

	m = &music[randInd]
	m.Options = music

	return m, nil
}

func (g *Game) getKeyboardMarkupForCurrentMusic() (keyboard tgbotapi.InlineKeyboardMarkup) {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, optionMusic := range g.currentMusic.Options {
		button := tgbotapi.NewInlineKeyboardButtonData(optionMusic.Title, g.inlineMessageId+":"+strconv.Itoa(g.questionNumber)+":"+strconv.Itoa(optionMusic.ID))
		row := tgbotapi.NewInlineKeyboardRow(button)
		rows = append(rows, row)
	}
	keyboard = tgbotapi.NewInlineKeyboardMarkup(rows...)
	return keyboard
}

func (g *Game) getTextWithHighScores() string {
	scorePlayers := g.getOrderedScoresDesc()
	text := ""
	for i, scorePlayer := range scorePlayers {
		text += strconv.Itoa(i+1) + ". " + scorePlayer.Name + ": " + strconv.Itoa(scorePlayer.Score) + "\n"
		if i > 10 {
			break
		}
	}
	return text
}

// sort highscores
func (g *Game) getOrderedScoresDesc() ScorePlayerList {
	pl := make(ScorePlayerList, len(g.players))
	i := 0
	for _, player := range g.players {
		pl[i] = player
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

type ScorePlayerList []*Player

func (p ScorePlayerList) Len() int           { return len(p) }
func (p ScorePlayerList) Less(i, j int) bool { return p[i].Score < p[j].Score }
func (p ScorePlayerList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// end of sort
