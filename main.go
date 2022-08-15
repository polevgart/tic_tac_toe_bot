package main

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "math/rand"
    "os"
    "strconv"
    "sync"
    "time"

    game "./game"
    telebot "github.com/tucnak/telebot"
)

type State string
const (
    Start State     = "Start"
    SearchingGame   = "SearchingGame"
    InGame          = "InGame"
    EndGame         = "EndGame"
)

type UserState struct {
    GameState game.GameState    `json:"game_state"`
    OpponentUser *telebot.User  `json:"opponent_user"`
    WhoMe game.Cell             `json:"who_me"`
    State State                 `json:"state"`
}

func (us *UserState) CanMeMakeMove() bool {
    log.Println("123", us.WhoMe, us.GameState.WhoTurn)
    return us.WhoMe == us.GameState.WhoTurn
}

func (us *UserState) MakeMove(i int, j int) bool {
    ok := us.GameState.MakeMove(i, j)
    return ok
}

func NewUser() UserState {
    us := UserState{
        GameState: game.GameState{
            Width: 8,
            Height: 8,
            WinLength: 5,
        },
        WhoMe: game.X,
        State: Start,
    }
    us.GameState.ResetGame()
    return us
}

type TicTacToeBotStorage struct {
    UserId2UserState map[int64]UserState
    UsersSearching map[int64]bool
    mutex sync.Mutex

    selectorConfirm *telebot.ReplyMarkup
    bot *telebot.Bot
}

func NewTicTacToeBotStorage() TicTacToeBotStorage {
    return TicTacToeBotStorage{
        UserId2UserState: make(map[int64]UserState),
        UsersSearching: make(map[int64]bool),
    }
}

func getUserId(context telebot.Context) int64 {
    return context.Sender().ID
}

func (botStorage *TicTacToeBotStorage) getUserState(userId int64) UserState {
    botStorage.mutex.Lock()
    defer botStorage.mutex.Unlock()
    userState, ok := botStorage.UserId2UserState[userId]
    if !ok {
        userState = NewUser()
        botStorage.UserId2UserState[userId] = userState
    }
    return userState
}

func (botStorage *TicTacToeBotStorage) setUserState(userId int64, userState UserState) {
    botStorage.mutex.Lock()
    defer botStorage.mutex.Unlock()
    botStorage.UserId2UserState[userId] = userState
}

func (botStorage *TicTacToeBotStorage) searchOpponents(userId int64) (int64, bool) {
    botStorage.mutex.Lock()
    defer botStorage.mutex.Unlock()
    log.Println("Searching opponent...")
    for key, _ := range botStorage.UsersSearching {
        if key == userId {
            log.Println("Opponent already searching...")
            return 0, false
        }
        delete(botStorage.UsersSearching, key)
        return key, true
    }
    log.Println("Opponent not found. Waiting...")
    botStorage.UsersSearching[userId] = true
    return 0, false
}

func Marshal(v interface{}) (io.Reader, error) {
    b, err := json.MarshalIndent(v, "", "\t")
    if err != nil {
        log.Println(err)
        return nil, err
    }
    return bytes.NewReader(b), nil
}

func Save(path string, v interface{}) error {
    f, err := os.Create(path)
    if err != nil {
        log.Println(err)
        return err
    }
    defer f.Close()
    r, err := Marshal(v)
    if err != nil {
        log.Println(err)
        return err
    }
    _, err = io.Copy(f, r)
    if err != nil {
        log.Println(err)
    }
    return err
}

func Unmarshal(r io.Reader, v interface{}) error {
    return json.NewDecoder(r).Decode(v)
}

func Load(path string, v interface{}) error {
    f, err := os.Open(path)
    if err != nil {
        // log.Fatal(err)
        return err
    }
    defer f.Close()
    return Unmarshal(f, v)
}

func makeEndGame(userMsg string, opponentMsg string, botStorage *TicTacToeBotStorage, context telebot.Context) {
    userId := getUserId(context)
    userState := botStorage.getUserState(userId)
    opponentState := botStorage.getUserState(userState.OpponentUser.ID)
    userState.State = EndGame
    botStorage.setUserState(userId, userState)

    opponentState.State = EndGame
    botStorage.setUserState(userState.OpponentUser.ID, opponentState)

    questionToNewGame := " Хотите начать новую игру?"
    if err := context.Send(userMsg + questionToNewGame, botStorage.selectorConfirm); err != nil {
        log.Fatal(err)
    }
    if _, err := botStorage.bot.Send(userState.OpponentUser, opponentMsg + questionToNewGame, botStorage.selectorConfirm); err != nil {
        log.Fatal(err)
    }
}

func constructButtonHandler(i int, j int, botStorage *TicTacToeBotStorage, selector *telebot.ReplyMarkup) func(context telebot.Context) error {
    return func(context telebot.Context) error {
        userId := getUserId(context)
        userState := botStorage.getUserState(userId)
        log.Println("Handle btn", i, j, userState)
        if !userState.CanMeMakeMove() {
            return context.Send("Сейчас не твой ход")
        }
        ok := userState.MakeMove(i, j)
        if !ok {
            return context.Send("Некорректный ход")
        }
        defer Save("save.json", botStorage)
        botStorage.setUserState(userId, userState)

        opponentState := botStorage.getUserState(userState.OpponentUser.ID)
        opponentState.MakeMove(i, j)
        botStorage.setUserState(userState.OpponentUser.ID, opponentState)
        if err := context.Send("Ожидаем ход соперника"); err != nil {
            log.Fatal(err)
        }

        if _, err := botStorage.bot.Send(userState.OpponentUser, userState.GameState.ShowBoardToString(), selector); err != nil {
            log.Fatal(err)
            return err
        }
        if userState.GameState.IsGameEnded {
            userMsg := ""
            opponentMsg := ""
            switch userState.GameState.WhoWin {
            case game.Empty:
                msg := "Ничья!"
                userMsg = msg
                opponentMsg = msg
            default:
                userMsg = "Вы выиграли!"
                opponentMsg = "Вы проиграли!"
                if userState.GameState.WhoWin != userState.WhoMe {
                    userMsg, opponentMsg = opponentMsg, userMsg
                }
            }

            makeEndGame(userMsg, opponentMsg, botStorage, context)
        }
        return nil
    }
}

func startSeachingOpponent(botStorage *TicTacToeBotStorage, context telebot.Context, selector *telebot.ReplyMarkup) error {
    userId := getUserId(context)
    userState := botStorage.getUserState(userId)
    userState.State = SearchingGame
    if err := context.Send("Ищу соперника..."); err != nil {
        return err
    }
    opponentUserId, found := botStorage.searchOpponents(userId)
    if found {
        log.Println("Opponent was found", opponentUserId)

        msgOpponentFound := "Соперник найден. Начинаем игру!"

        fig := []game.Cell{game.X, game.O}
        rand.Shuffle(len(fig), func(i, j int) { fig[i], fig[j] = fig[j], fig[i] })

        userState.State = InGame
        userState.OpponentUser = &telebot.User{ID: opponentUserId}
        userState.GameState.ResetGame()
        userState.WhoMe = fig[0]
        botStorage.setUserState(userId, userState)

        context.Send(msgOpponentFound)
        if userState.WhoMe == game.X {
            context.Send(userState.GameState.ShowBoardToString(), selector)
        } else {
            context.Send("Ожидаем ход соперника")
        }

        opponentUserState := botStorage.getUserState(opponentUserId)
        opponentUserState.State = InGame
        opponentUserState.OpponentUser = &telebot.User{ID: userId}
        opponentUserState.GameState.ResetGame()
        opponentUserState.WhoMe = fig[1]
        botStorage.setUserState(opponentUserId, opponentUserState)

        botStorage.bot.Send(userState.OpponentUser, msgOpponentFound)
        if opponentUserState.WhoMe == game.X {
            botStorage.bot.Send(userState.OpponentUser, userState.GameState.ShowBoardToString(), selector)
        } else {
            botStorage.bot.Send(userState.OpponentUser, "Ожидаем ход соперника")
        }
    }
    return nil
}

func main() {
    rand.Seed(time.Now().UnixNano())

	pref := telebot.Settings{
		Token:  os.Getenv("TELEGRAM_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

    botStorage := NewTicTacToeBotStorage()
    Load("save.json", &botStorage)

    selectorConfirm := &telebot.ReplyMarkup{}
    yesButton := selectorConfirm.Data("Да", "yes")
    noButton := selectorConfirm.Data("Нет", "no")
    selectorConfirm.Inline(selectorConfirm.Row(yesButton,noButton))

    botStorage.bot = bot
    botStorage.selectorConfirm = selectorConfirm

    selector := &telebot.ReplyMarkup{}
    buttonsHeight := 8
    buttonsWidth := 8
    buttons := make([]telebot.Row, buttonsHeight)
    for i := 0; i < buttonsHeight; i++ {
        buttons[i] = make([]telebot.Btn, buttonsWidth)
        for j := 0; j < buttonsWidth; j++ {
            buttons[i][j] = selector.Data("🌫", strconv.Itoa(i * buttonsWidth + j))
            bot.Handle(&buttons[i][j], constructButtonHandler(i, j, &botStorage, selector))
        }
    }
    selector.Inline(buttons...)

    bot.Handle(&yesButton, func(context telebot.Context) error {
        defer Save("save.json", botStorage)
        userId := getUserId(context)
        userState := botStorage.getUserState(userId)
        switch userState.State {
        case Start:
            startSeachingOpponent(&botStorage, context, selector)
        case EndGame:
            startSeachingOpponent(&botStorage, context, selector)
        }
        return nil
    })
    bot.Handle(&noButton, func(context telebot.Context) error {
        userState := botStorage.getUserState(getUserId(context))
        switch userState.State {
        case Start:
            return context.Send("Окей, тогда до связи!")
        }
        return nil
    })


    printHelloMsg := func(context telebot.Context) error {
        defer Save("save.json", botStorage)
        userState := botStorage.getUserState(getUserId(context))
        log.Println("Hello!", userState)
        return context.Send("Привет! Хочешь сыграть в крестики нолики?", selectorConfirm)
    }

    log.Println("Started")
    bot.Handle("/hello", printHelloMsg)
    bot.Handle("/start", printHelloMsg)
    bot.Handle("/resign", func(context telebot.Context) error {
        userState := botStorage.getUserState(getUserId(context))
        if userState.State != InGame {
            context.Send("Вы не в игре, для того чтобы сдаться.")
            return nil
        }

        defer Save("save.json", botStorage)
        makeEndGame("Вы сдались.", "Соперник сдался.", &botStorage, context)
        return nil
    })

	bot.Start()
}
