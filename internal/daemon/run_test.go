package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/benyamin-git/kumiho/internal/api"
)

func TestControlErrorReply(t *testing.T) {
	region := &api.RemoteError{Code: api.CodeRegionBlocked, Message: "This region is blocking Mozilla's servers (HTTP 451)."}
	code, msg := controlErrorReply(fmt.Errorf("locations: %w", region))
	if code != api.CodeRegionBlocked || msg != region.Message {
		t.Fatalf("region reply = %q, %q", code, msg)
	}

	code, msg = controlErrorReply(errors.New("boom"))
	if code != api.CodeInternal || msg != "boom" {
		t.Fatalf("internal reply = %q, %q", code, msg)
	}
}
