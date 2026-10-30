package gateway

import (
	"context"
	"net/http"
	"time"
)

func boundRequestIO(ctx context.Context, cancel context.CancelFunc, w http.ResponseWriter, r *http.Request, deadline time.Time) func() {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline.Add(finalizationTimeout))
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = controller.SetReadDeadline(time.Now())
		if r.Body != nil {
			_ = r.Body.Close()
		}
		close(done)
	})
	return func() {
		cancel()
		if !stop() {
			<-done
		}
		_ = controller.SetReadDeadline(time.Time{})
		_ = controller.SetWriteDeadline(time.Time{})
	}
}
