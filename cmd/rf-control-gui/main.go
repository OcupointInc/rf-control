package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/OcupointInc/rf-control/internal/cli"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 {
		cli.Main()
		return
	}
	detachConsole()
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "Ocupoint RF Control",
		Width:     1280,
		Height:    820,
		MinWidth:  1040,
		MinHeight: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 17, B: 29, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		fmt.Println("rf-control-gui:", err)
	}
}
