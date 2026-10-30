package main

import (
	"encoding/json"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
)

type nativeSmokeReadout struct {
	Ready      bool   `json:"ready"`
	PID        int    `json:"pid"`
	URL        string `json:"url"`
	Chrome     bool   `json:"chrome"`
	Typography bool   `json:"typography"`
	InsetLeft  string `json:"inset_left"`
	InsetRight string `json:"inset_right"`
	Error      string `json:"error,omitempty"`
}

func nativeSmokeResultPath() string { return os.Getenv("ROUTER_FORGE_NATIVE_SMOKE_RESULT") }

// Opt-in acceptance mode observes the real renderer after its generated bridge calls.
// It never creates a run, sends a model request, or alters the ordinary application flow.
func verifyNativeWindow(win *mygo.Window, resultPath string) {
	result := nativeSmokeReadout{Error: "native renderer did not attach within 45 seconds"}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		readout, err := mygo.EvalAs[nativeSmokeReadout](win.Page(), `
const node = document.querySelector('[data-native-backend="true"]');
const titlebar = document.querySelector('[data-slot="desktop-titlebar"]');
const root = getComputedStyle(document.documentElement);
return {
  ready: node?.dataset.apiReady === 'true', pid: Number(node?.dataset.backendPid || 0), url: node?.dataset.backendUrl || '',
  chrome: !!titlebar && titlebar.getBoundingClientRect().height >= 40 && getComputedStyle(titlebar).getPropertyValue('--app-region').trim() === 'drag',
  typography: getComputedStyle(document.body).fontSize === '12px' && !!node?.querySelector('h1') && getComputedStyle(node.querySelector('h1')).fontSize === '12px',
  inset_left: root.getPropertyValue('--mygo-titlebar-inset-left').trim(),
  inset_right: root.getPropertyValue('--mygo-titlebar-inset-right').trim()
};`)
		if err == nil && readout.Ready && readout.PID > 0 && readout.Chrome && readout.Typography {
			result = readout
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	data, _ := json.Marshal(result)
	_ = os.WriteFile(resultPath, data, 0600)
	if hold, err := strconv.Atoi(os.Getenv("ROUTER_FORGE_NATIVE_SMOKE_HOLD_SECONDS")); err == nil && hold > 0 && hold <= 120 {
		time.Sleep(time.Duration(hold) * time.Second)
	}
	mygo.App.Quit()
}
