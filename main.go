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

type UserStorage struct {
    UserId2UserStorage map[int64]UserState
    UsersSearching map[int64]bool
    mutex sync.Mutex
}

func NewUserStorage() UserStorage {
    return UserStorage{
        UserId2UserStorage: make(map[int64]UserState),
        UsersSearching: make(map[int64]bool),
    }
}

func getUserId(context telebot.Context) int64 {
    return context.Sender().ID
}

func (userStorage *UserStorage) getUserState(userId int64) UserState {
    userStorage.mutex.Lock()
    defer userStorage.mutex.Unlock()
    userState, ok := userStorage.UserId2UserStorage[userId]
    if !ok {
        userState = NewUser()
        userStorage.UserId2UserStorage[userId] = userState
    }
    return userState
}

func (userStorage *UserStorage) setUserState(userId int64, userState UserState) {
    userStorage.mutex.Lock()
    defer userStorage.mutex.Unlock()
    userStorage.UserId2UserStorage[userId] = userState
}

func (userStorage *UserStorage) searchOpponents(userId int64) (int64, bool) {
    userStorage.mutex.Lock()
    defer userStorage.mutex.Unlock()
    log.Println("Searching opponent...")
    for key, _ := range userStorage.UsersSearching {
        if key == userId {
            log.Println("Opponent already searching...")
            return 0, false
        }
        delete(userStorage.UsersSearching, key)
        return key, true
    }
    log.Println("Opponent not found. Waiting...")
    userStorage.UsersSearching[userId] = true
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

func makeEndGame(userMsg string, opponentMsg string, userStorage *UserStorage,
                 context telebot.Context, bot *telebot.Bot, selectorConfirm *telebot.ReplyMarkup) {
    userId := getUserId(context)
    userState := userStorage.getUserState(userId)
    opponentState := userStorage.getUserState(userState.OpponentUser.ID)
    userState.State = EndGame
    userStorage.setUserState(userId, userState)

    opponentState.State = EndGame
    userStorage.setUserState(userState.OpponentUser.ID, opponentState)

    questionToNewGame := " Хотите начать новую игру?"
    if err := context.Send(userMsg + questionToNewGame, selectorConfirm); err != nil {
        log.Fatal(err)
    }
    if _, err := bot.Send(userState.OpponentUser, opponentMsg + questionToNewGame, selectorConfirm); err != nil {
        log.Fatal(err)
    }
}

func constructButtonHandler(i int, j int, userStorage *UserStorage, bot *telebot.Bot,
                            selector *telebot.ReplyMarkup, selectorConfirm *telebot.ReplyMarkup) func(context telebot.Context) error {
    return func(context telebot.Context) error {
        userId := getUserId(context)
        userState := userStorage.getUserState(userId)
        log.Println("Handle btn", i, j, userState)
        if !userState.CanMeMakeMove() {
            return context.Send("Сейчас не твой ход")
        }
        ok := userState.MakeMove(i, j)
        if !ok {
            return context.Send("Некорректный ход")
        }
        defer Save("save.json", userStorage)
        userStorage.setUserState(userId, userState)

        opponentState := userStorage.getUserState(userState.OpponentUser.ID)
        opponentState.MakeMove(i, j)
        userStorage.setUserState(userState.OpponentUser.ID, opponentState)
        if err := context.Send("Ожидаем ход соперника"); err != nil {
            log.Fatal(err)
        }

        if _, err := bot.Send(userState.OpponentUser, userState.GameState.ShowBoardToString(), selector); err != nil {
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

            makeEndGame(userMsg, opponentMsg, userStorage, context, bot, selectorConfirm)
        }
        return nil
    }
}

func startSeachingOpponent(userStorage *UserStorage, context telebot.Context, bot *telebot.Bot, selector *telebot.ReplyMarkup) error {
    userId := getUserId(context)
    userState := userStorage.getUserState(userId)
    userState.State = SearchingGame
    if err := context.Send("Ищу соперника..."); err != nil {
        return err
    }
    opponentUserId, found := userStorage.searchOpponents(userId)
    if found {
        log.Println("Opponent was found", opponentUserId)

        msgOpponentFound := "Соперник найден. Начинаем игру!"

        fig := []game.Cell{game.X, game.O}
        rand.Shuffle(len(fig), func(i, j int) { fig[i], fig[j] = fig[j], fig[i] })

        userState.State = InGame
        userState.OpponentUser = &telebot.User{ID: opponentUserId}
        userState.GameState.ResetGame()
        userState.WhoMe = fig[0]
        userStorage.setUserState(userId, userState)

        context.Send(msgOpponentFound)
        if userState.WhoMe == game.X {
            context.Send(userState.GameState.ShowBoardToString(), selector)
        } else {
            context.Send("Ожидаем ход соперника")
        }

        opponentUserState := userStorage.getUserState(opponentUserId)
        opponentUserState.State = InGame
        opponentUserState.OpponentUser = &telebot.User{ID: userId}
        opponentUserState.GameState.ResetGame()
        opponentUserState.WhoMe = fig[1]
        userStorage.setUserState(opponentUserId, opponentUserState)

        bot.Send(userState.OpponentUser, msgOpponentFound)
        if opponentUserState.WhoMe == game.X {
            bot.Send(userState.OpponentUser, userState.GameState.ShowBoardToString(), selector)
        } else {
            bot.Send(userState.OpponentUser, "Ожидаем ход соперника")
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

    userStorage := NewUserStorage()
    Load("save.json", &userStorage)

    selectorConfirm := &telebot.ReplyMarkup{}
    yesButton := selectorConfirm.Data("Да", "yes")
    noButton := selectorConfirm.Data("Нет", "no")
    selectorConfirm.Inline(selectorConfirm.Row(yesButton,noButton))

    selector := &telebot.ReplyMarkup{}
    buttonsHeight := 8
    buttonsWidth := 8
    buttons := make([]telebot.Row, buttonsHeight)
    for i := 0; i < buttonsHeight; i++ {
        buttons[i] = make([]telebot.Btn, buttonsWidth)
        for j := 0; j < buttonsWidth; j++ {
            buttons[i][j] = selector.Data("🌫", strconv.Itoa(i * buttonsWidth + j))
            bot.Handle(&buttons[i][j], constructButtonHandler(i, j, &userStorage, bot, selector, selectorConfirm))
        }
    }
    selector.Inline(buttons...)

    bot.Handle(&yesButton, func(context telebot.Context) error {
        defer Save("save.json", userStorage)
        userId := getUserId(context)
        userState := userStorage.getUserState(userId)
        switch userState.State {
        case Start:
            startSeachingOpponent(&userStorage, context, bot, selector)
        case EndGame:
            startSeachingOpponent(&userStorage, context, bot, selector)
        }
        return nil
    })
    bot.Handle(&noButton, func(context telebot.Context) error {
        userState := userStorage.getUserState(getUserId(context))
        switch userState.State {
        case Start:
            return context.Send("Окей, тогда до связи!")
        }
        return nil
    })


    printHelloMsg := func(context telebot.Context) error {
        defer Save("save.json", userStorage)
        userState := userStorage.getUserState(getUserId(context))
        log.Println("Hello!", userState)
        return context.Send("Привет! Хочешь сыграть в крестики нолики?", selectorConfirm)
    }

    log.Println("Started")
    bot.Handle("/hello", printHelloMsg)
    bot.Handle("/start", printHelloMsg)
    bot.Handle("/resign", func(context telebot.Context) error {
        userState := userStorage.getUserState(getUserId(context))
        if userState.State != InGame {
            context.Send("Вы не в игре, для того чтобы сдаться.")
            return nil
        }

        defer Save("save.json", userStorage)
        makeEndGame("Вы сдались.", "Соперник сдался.", &userStorage, context, bot, selectorConfirm)
        return nil
    })

	bot.Start()
}
