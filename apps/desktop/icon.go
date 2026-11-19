package main

import (
	_ "embed"

	"github.com/egoist/mygo"
)

//go:embed resources/icon.png
var applicationIcon []byte

func setApplicationIcon(win *mygo.Window) error {
	return win.SetIcon(applicationIcon)
}
