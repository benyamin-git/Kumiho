package cli

import (
	"fmt"
	"io"
)

const guideText = `Blocked region

Your network is in a region where Mozilla blocks part of the Firefox VPN
service. Kumiho can still work. Try these paths in order.

Option 1: Try Prepare now (recommended)

  1. Sign in with a password:  kumiho login --password
     Password sign-in works on blocked networks. Browser sign-in still
     needs another VPN or another device.
  2. Prepare Kumiho:           kumiho prepare
     Kumiho tries Mozilla's reachable sibling hosts and Guardian's
     alternate path.
  3. If the pass is ready:     kumiho connect
  4. If prepare still reports HTTP 451, try Option 2 or Option 3.

Option 2: I have another VPN

  1. Turn on your other VPN.
  2. Sign in to Kumiho:        kumiho login
  3. Prepare Kumiho:           kumiho prepare
  4. When Kumiho says Prepared, turn off the other VPN.
  5. Connect:                  kumiho connect

  You need the other VPN again only if Kumiho stays off for more than
  10 minutes.

Option 3: Use your own relay

  1. Save its URL:             kumiho settings set relay_url https://relay.example
  2. Optional token:           kumiho settings set relay_token
  3. Test it:                  kumiho relay test
  4. Prepare, then connect:    kumiho prepare && kumiho connect

  Kumiho sends only blocked Mozilla requests through the relay. VPN traffic
  never goes through it. The token is optional, and the relay does not proxy
  the browser sign-in page. Deployment is in docs/relay.md.

Details: Mozilla returns HTTP 451 for the blocked hosts. Run
` + "`kumiho doctor`" + ` to see which ones.
`

func runGuide(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "kumiho guide: unexpected argument %q\n", args[0])
		return 2
	}
	fmt.Fprint(stdout, guideText)
	return 0
}
