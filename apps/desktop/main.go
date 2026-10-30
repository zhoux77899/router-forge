package main

import (
	"flag"
	"log"
	"path/filepath"

	backend "github.com/zhoux77899/router-forge/internal/runtime"

	"github.com/egoist/mygo"
)

func main() {
	dataDir := flag.String("data-dir", "", "absolute local data directory (overrides ROUTER_FORGE_DATA_DIR)")
	flag.Parse()
	console := newConsole()
	if *dataDir != "" {
		if !filepath.IsAbs(*dataDir) {
			log.Fatal("--data-dir must be an absolute path")
		}
		console.options = func() (backend.Options, error) { return locateBackendAt(*dataDir) }
		mygo.App.SetPath(mygo.PathUserData, *dataDir)
		mygo.App.SetPath(mygo.PathCache, filepath.Join(*dataDir, "renderer-cache"))
		mygo.App.SetPath(mygo.PathLogs, filepath.Join(*dataDir, "renderer-logs"))
	}
	mygo.Bind(console)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Router Forge",
			Width:     1360,
			Height:    880,
			MinWidth:  980,
			MinHeight: 640,
			URL:       "/",
		})
		if result := nativeSmokeResultPath(); result != "" {
			go verifyNativeWindow(win, result)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
