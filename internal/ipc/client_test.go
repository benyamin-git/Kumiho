package ipc

import (
	"encoding/json"
	"net"
	"runtime"
	"testing"
)

// TestCloseWhileStreamingEventsDoesNotPanic exercises the race between the
// read loop forwarding an unsolicited event and Close closing the channel.
func TestCloseWhileStreamingEventsDoesNotPanic(t *testing.T) {
	for i := 0; i < 500; i++ {
		clientSide, serverSide := net.Pipe()
		c := newClient(clientSide)

		drained := make(chan struct{})
		go func() {
			for range c.Events() {
			}
			close(drained)
		}()

		written := make(chan struct{})
		go func() {
			enc := json.NewEncoder(serverSide)
			for j := 0; j < 16; j++ {
				if err := enc.Encode(&Envelope{Type: TypeStatus}); err != nil {
					break
				}
			}
			close(written)
		}()

		runtime.Gosched()
		_ = c.Close()
		_ = serverSide.Close()
		<-written
		<-drained
	}
}
