package lib

import (
    "fmt"
    "log"
)

type Cell string
const (
    X Cell = "❌"
    O      = "⭕️"
    Empty  = "🌫"
)

type State string
const (
    Start State     = "Start"
    InGame          = "InGame"
    End             = "End"
)

type GameState struct {
    Board [][]Cell
    Width int
    Height int
    WinLength int
    State State

    WhoTurn Cell
    IsGameEnded bool
    WhoWin Cell
}

func (gs *GameState) CheckEnd() {
    hasEmpty := false
    for i := 0; i < gs.Height; i++ {
        for j := 0; j < gs.Width; j++ {
            if whoThis := gs.Board[i][j]; whoThis != Empty {
                row, col, diag1, diag2 := true, true, true, true
                for k := 1; k < gs.WinLength; k++ {
                    row = row && i + k < gs.Height && gs.Board[i + k][j] == whoThis
                    col = col && j + k < gs.Width && gs.Board[i][j + k] == whoThis
                    diag1 = diag1 && i + k < gs.Height && j + k < gs.Width && gs.Board[i + k][j + k] == whoThis
                    diag2 = diag2 && i - k >= 0 && j + k < gs.Width && gs.Board[i - k][j + k] == whoThis
                }
                if row || col || diag1 || diag2 {
                    log.Println("Kek", row, col, diag1, diag2)
                    gs.IsGameEnded = true
                    gs.WhoWin = whoThis
                    return
                }
            } else {
                hasEmpty = true
            }
        }
    }
    if !hasEmpty {
        gs.IsGameEnded = true
        gs.WhoWin = Empty
    }
    return
}

func (gs *GameState) MakeMove(i int, j int) bool {
    if gs.IsGameEnded || i < 0 || i >= gs.Height || j < 0 || j >= gs.Width || gs.Board[i][j] != Empty  {
        return false
    }
    gs.Board[i][j] = gs.WhoTurn
    gs.CheckEnd()
    if gs.WhoTurn == X {
        gs.WhoTurn = O
    } else {
        gs.WhoTurn = X
    }
    return true
}

func (gs *GameState) ResetGame() bool {
    gs.Board = make([][]Cell, gs.Height)
    for i := 0; i < gs.Height; i++ {
        gs.Board[i] = make([]Cell, gs.Width)
        for j := 0; j < gs.Width; j++ {
            gs.Board[i][j] = Empty
        }
    }
    gs.WhoTurn = X
    gs.IsGameEnded = false
    return true
}

func (gs *GameState) ValidateParams() bool {
    return gs.Width >= gs.WinLength && gs.Height >= gs.WinLength
}

func (gs *GameState) ShowBoardToString() string {
    result := ""
    for i := 0; i < gs.Height; i++ {
        for j := 0; j < gs.Width; j++ {
            result += string(gs.Board[i][j])
        }
        result += "\n"
    }
    result += "\n"
    return result
}

func (gs *GameState) ShowBoardOnConsole() bool {
    log.Println("Show on console")
    fmt.Printf(gs.ShowBoardToString())
    return true
}

func ReadMoveFromConsole() (int, int, bool) {
    var i, j int
    _, err := fmt.Scanf("%d %d", &i, &j)
    if err != nil {
        return -1, -1, false
    }
    return i, j, true
}

func RunConsoleGameLoop(gs GameState) {
    if ok := gs.ValidateParams(); !ok {
        log.Fatal("Invalid params")
        return
    }
    for i := 1; i != 0; {
        gs.ResetGame()
        log.Println(gs)
        for gs.IsGameEnded {
            x, y, okRead := ReadMoveFromConsole()
            okSet := gs.MakeMove(x, y)
            if !okRead || !okSet {
                fmt.Printf("Неправильные координаты %s %s\n", okRead, okSet)
                continue
            }
            gs.ShowBoardOnConsole()
        }

        switch gs.WhoWin {
        case Empty:
            fmt.Printf("Ничья\n")
        case X:
            fmt.Printf("Победили крестики\n")
        case O:
            fmt.Printf("Победили нолики\n")
        }

        fmt.Printf("Введите 1 чтобы сыграть еще раз или 0 для выхода ")
        fmt.Scanf("%d", &i)
    }
}

func Main() {
    gs := GameState{
        Width: 3,
        Height: 3,
        WinLength: 3,
    }
    RunConsoleGameLoop(gs)
}
