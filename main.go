package main

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "math/rand"
    "os"
    "strconv"
    "strings"
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

type UserCustomization struct {
    X, O, Empty, XLast, OLast string
}

type UserState struct {
    GameState game.GameState
    OpponentUserID int64
    User *telebot.User
    WhoMe game.Cell
    State State

    Customization UserCustomization
    Selector *telebot.ReplyMarkup
    LastX, LastY int

    BadMoveMessages []*telebot.StoredMessage
    LastBotMsg *telebot.StoredMessage
    LastBotText string

    mutex sync.Mutex
}

func (us *UserState) CanMeMakeMove() bool {
    us.mutex.Lock()
    defer us.mutex.Unlock()
    return us.WhoMe == us.GameState.WhoTurn
}

func (us *UserState) MakeMove(i int, j int) bool {
    us.mutex.Lock()
    defer us.mutex.Unlock()
    ok := us.GameState.MakeMove(i, j)
    return ok
}

func (us *UserState) ResetGame() {
    us.mutex.Lock()
    defer us.mutex.Unlock()
    us.LastX = -1
    us.LastY = -1
    us.GameState.ResetGame()
    for i := range us.Selector.InlineKeyboard {
        for j := range us.Selector.InlineKeyboard[i] {
            us.Selector.InlineKeyboard[i][j].Text = us.Customization.Empty
        }
    }
}

func constructSelectorBoard(buttonsWidth, buttonsHeight int) *telebot.ReplyMarkup {
    selector := &telebot.ReplyMarkup{}
    buttons := make([]telebot.Row, buttonsHeight)
    for i := 0; i < buttonsHeight; i++ {
        buttons[i] = make([]telebot.Btn, buttonsWidth)
        for j := 0; j < buttonsWidth; j++ {
            buttons[i][j] = selector.Data("🌫", strconv.Itoa(i * buttonsWidth + j))
        }
    }
    selector.Inline(buttons...)
    return selector
}

func NewUser() UserState {
    us := UserState{
        GameState: game.GameState{
            Width:     8,
            Height:    8,
            WinLength: 5,
        },
        WhoMe: game.X,
        State: Start,
        Customization: UserCustomization{
            X:     "❌",
            O:     "🔴",
            Empty: "🌫",
            XLast: "❎",
            OLast: "🟢",
        },
        LastX: -1,
        LastY: -1,
    }
    us.Selector = constructSelectorBoard(us.GameState.Width, us.GameState.Height)
    us.ResetGame()
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

func (botStorage *TicTacToeBotStorage) RegisterUser(context telebot.Context) UserState {
    user := context.Sender()
    userState := botStorage.getUserState(user.ID)
    if userState.User != nil {
        return userState
    }

    log.Println("Registering new user", user.ID)
    userState.User = user
    botStorage.setUserState(user.ID, userState)

    Save("save.json", botStorage)
    return userState
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

var (
    saveMutex = sync.Mutex{}
)

func Save(path string, v interface{}) error {
    saveMutex.Lock()
    defer saveMutex.Unlock()
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

type IsNewMessage bool
const (
    NewMessage  IsNewMessage = true
    EditPreviousMessage      = false
)

type IsMessageEditable bool
const (
    MessageEditable IsMessageEditable = true
    MessageNotEditable                = false
)

func SendEditable(botStorage *TicTacToeBotStorage, userState *UserState, newMsg IsNewMessage, editableMsg IsMessageEditable,
                  what string,  opts ...interface{}) error {
    defer Save("save.json", botStorage)

    user := userState.User
    if newMsg && userState.LastBotMsg != nil {
        log.Println("New message")
        botStorage.bot.Edit(userState.LastBotMsg, userState.LastBotText)
    } else if userState.LastBotMsg != nil {
        log.Println("Edit last message", userState.LastBotText, "to", what)
        userState.LastBotText = what
        _, err := botStorage.bot.Edit(userState.LastBotMsg, what, opts...)
        if !editableMsg {
            userState.LastBotMsg = nil
            userState.LastBotText = ""
        }
        botStorage.setUserState(user.ID, *userState)
        return err
    }

    log.Println("Send new message", what)
    m, err := botStorage.bot.Send(telebot.Recipient(user), what, opts...)
    if err != nil {
        return err
    }
    log.Println("Sending complete")
    if editableMsg {
        messageID, chatID := m.MessageSig()
        userState.LastBotMsg = &telebot.StoredMessage{MessageID: messageID, ChatID: chatID}
        userState.LastBotText = what
    } else {
        userState.LastBotMsg = nil
        userState.LastBotText = ""
    }
    botStorage.setUserState(user.ID, *userState)
    return nil
}

func makeEndGame(userMsg string, opponentMsg string, botStorage *TicTacToeBotStorage, context telebot.Context) {
    userId := getUserId(context)
    userState := botStorage.getUserState(userId)
    opponentState := botStorage.getUserState(userState.OpponentUserID)
    userState.State = EndGame
    botStorage.setUserState(userId, userState)

    opponentState.State = EndGame
    botStorage.setUserState(userState.OpponentUserID, opponentState)

    SendEditable(botStorage, &userState, EditPreviousMessage, MessageNotEditable, userState.GameState.ShowBoardToString())
    SendEditable(botStorage, &opponentState, EditPreviousMessage, MessageNotEditable, opponentState.GameState.ShowBoardToString())

    questionToNewGame := " Хотите начать новую игру?"
    if err := SendEditable(botStorage, &userState, NewMessage, MessageEditable, userMsg + questionToNewGame, botStorage.selectorConfirm); err != nil {
        log.Fatal(err)
    }
    if err := SendEditable(botStorage, &opponentState, NewMessage, MessageEditable,
                              opponentMsg + questionToNewGame, botStorage.selectorConfirm); err != nil {
        log.Fatal(err)
    }
}

func constructButtonHandler(i int, j int, botStorage *TicTacToeBotStorage) func(context telebot.Context) error {
    return func(context telebot.Context) error {
        userId := getUserId(context)
        userState := botStorage.getUserState(userId)
        log.Println("Handle btn", i, j, userState)
        if !userState.CanMeMakeMove() {
            m, err := botStorage.bot.Send(telebot.Recipient(userState.User), "Сейчас не твой ход")
            if err == nil {
                messageID, chatID := m.MessageSig()
                userState.BadMoveMessages = append(userState.BadMoveMessages, &telebot.StoredMessage{MessageID: messageID, ChatID: chatID})
                botStorage.setUserState(userId, userState)
            }
            return err
        }
        ok := userState.MakeMove(i, j)
        if !ok {
            m, err := botStorage.bot.Send(telebot.Recipient(userState.User), "Некорректный ход")
            if err == nil {
                messageID, chatID := m.MessageSig()
                userState.BadMoveMessages = append(userState.BadMoveMessages, &telebot.StoredMessage{MessageID: messageID, ChatID: chatID})
                botStorage.setUserState(userId, userState)
            }
            return err
        }
        defer Save("save.json", botStorage)
        for _, msg := range userState.BadMoveMessages {
            botStorage.bot.Delete(msg)
        }
        botStorage.setUserState(userId, userState)

        opponentState := botStorage.getUserState(userState.OpponentUserID)
        opponentState.MakeMove(i, j)
        botStorage.setUserState(userState.OpponentUserID, opponentState)
        if err := SendEditable(botStorage, &userState, EditPreviousMessage, MessageEditable, "Ожидаем ход соперника"); err != nil {
            log.Fatal(err)
        }

        opponentState.LastX = i
        opponentState.LastY = j
        if userState.WhoMe == game.X {
            opponentState.Selector.InlineKeyboard[i][j].Text = opponentState.Customization.XLast
            userState.Selector.InlineKeyboard[i][j].Text = userState.Customization.X
            if userState.LastX != -1 {
                userState.Selector.InlineKeyboard[userState.LastX][userState.LastY].Text = userState.Customization.O
            }
        } else {
            opponentState.Selector.InlineKeyboard[i][j].Text = opponentState.Customization.OLast
            userState.Selector.InlineKeyboard[i][j].Text = userState.Customization.O
            if userState.LastX != -1 {
                userState.Selector.InlineKeyboard[userState.LastX][userState.LastY].Text = userState.Customization.X
            }
        }
        botStorage.setUserState(userState.OpponentUserID, opponentState)

        if err := SendEditable(botStorage, &opponentState, EditPreviousMessage, MessageEditable, "Ваш ход", opponentState.Selector); err != nil {
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

func startSeachingOpponent(botStorage *TicTacToeBotStorage, context telebot.Context) error {
    userId := getUserId(context)
    userState := botStorage.getUserState(userId)
    userState.State = SearchingGame
    if err := SendEditable(botStorage, &userState, EditPreviousMessage, MessageEditable, "Ищу соперника..."); err != nil {
        return err
    }
    opponentUserId, found := botStorage.searchOpponents(userId)
    if found {
        log.Println("Opponent was found", opponentUserId)

        msgOpponentFound := "Соперник найден. Начинаем игру!"

        fig := []game.Cell{game.X, game.O}
        rand.Shuffle(len(fig), func(i, j int) { fig[i], fig[j] = fig[j], fig[i] })

        userState.State = InGame
        userState.OpponentUserID = opponentUserId
        userState.WhoMe = fig[0]
        botStorage.setUserState(userId, userState)

        SendEditable(botStorage, &userState, EditPreviousMessage, MessageNotEditable, msgOpponentFound)
        if userState.WhoMe == game.X {
            SendEditable(botStorage, &userState, EditPreviousMessage, MessageEditable, "Ваш ход", userState.Selector)
        } else {
            SendEditable(botStorage, &userState, EditPreviousMessage, MessageEditable, "Ожидаем ход соперника")
        }

        opponentUserState := botStorage.getUserState(opponentUserId)
        opponentUserState.State = InGame
        opponentUserState.OpponentUserID = userId
        opponentUserState.WhoMe = fig[1]
        botStorage.setUserState(opponentUserId, opponentUserState)

        SendEditable(botStorage, &opponentUserState, EditPreviousMessage, MessageNotEditable, msgOpponentFound)
        if opponentUserState.WhoMe == game.X {
            SendEditable(botStorage, &opponentUserState, EditPreviousMessage, MessageEditable, "Ваш ход", opponentUserState.Selector)
        } else {
            SendEditable(botStorage, &opponentUserState, EditPreviousMessage, MessageEditable, "Ожидаем ход соперника")
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


    buttonsHeight := 8
    buttonsWidth := 8
    buttons := constructSelectorBoard(buttonsWidth, buttonsHeight).InlineKeyboard
    for i := 0; i < buttonsHeight; i++ {
        for j := 0; j < buttonsWidth; j++ {
            bot.Handle(&buttons[i][j], constructButtonHandler(i, j, &botStorage))
        }
    }

    bot.Handle(&yesButton, func(context telebot.Context) error {
        defer Save("save.json", botStorage)
        userId := getUserId(context)
        userState := botStorage.getUserState(userId)
        userState.ResetGame()
        botStorage.setUserState(userId, userState)
        switch userState.State {
        case Start, EndGame:
            startSeachingOpponent(&botStorage, context)
        }
        return nil
    })
    bot.Handle(&noButton, func(context telebot.Context) error {
        userState := botStorage.getUserState(getUserId(context))
        switch userState.State {
        case Start, EndGame:
            return SendEditable(&botStorage, &userState, NewMessage, MessageNotEditable, "Окей, тогда до связи!")
        }
        return nil
    })

    printHelloMsg := func(context telebot.Context) error {
        userState := botStorage.RegisterUser(context)
        log.Println("Hello!", userState)
        return SendEditable(&botStorage, &userState, NewMessage, MessageEditable,
                            "Привет! Хочешь сыграть в крестики нолики?", selectorConfirm)
    }

    log.Println("Started")
    bot.Handle("/hello", printHelloMsg)
    bot.Handle("/start", printHelloMsg)
    bot.Handle("/resign", func(context telebot.Context) error {
        userState := botStorage.RegisterUser(context)
        if userState.State != InGame {
            SendEditable(&botStorage, &userState, NewMessage, MessageNotEditable, "Вы не в игре, для того чтобы сдаться.")
            return nil
        }

        defer Save("save.json", botStorage)
        makeEndGame("Вы сдались.", "Соперник сдался.", &botStorage, context)
        return nil
    })
    bot.Handle("/help", func(context telebot.Context) error {
        userState := botStorage.RegisterUser(context)
        return SendEditable(&botStorage, &userState, NewMessage, MessageNotEditable, strings.Join([]string{
            "/help - помощь",
            "/resign - сдаться в текущей игре",
            "/start - начать общение с ботом",
        }, "\n"))
    })

	bot.Start()
}
