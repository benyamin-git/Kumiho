package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/benyamin-git/kumiho/internal/ipc"
	"github.com/benyamin-git/kumiho/internal/platform/auto"
)

// dialDaemon connects to the local daemon or prints how to start it.
func dialDaemon(stderr io.Writer) (*ipc.Client, int) {
	endpoint := auto.Current().ControlEndpoint()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := (ipc.UnixTransport{}).Dial(ctx, endpoint)
	if err != nil {
		fmt.Fprintf(stderr, "kumiho: cannot reach the daemon at %s\n", endpoint)
		fmt.Fprintln(stderr, "start it with: sudo systemctl start kumiho  (or run: kumiho daemon)")
		return nil, 1
	}
	return client, 0
}

// callCtx bounds interactive requests (login may involve several round trips).
func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}
