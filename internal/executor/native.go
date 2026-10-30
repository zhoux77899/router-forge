package executor

import (
	"net/http"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type nativeWorker struct {
	gateway       *http.Client
	mu            sync.Mutex
	agents        []domain.Agent
	agentKey      string
	agentTime     time.Time
	clientFactory func(domain.Environment) (*http.Client, error)
}

func newNativeWorker(client *http.Client) *nativeWorker { return &nativeWorker{gateway: client} }
