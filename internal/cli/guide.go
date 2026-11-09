package cli

import (
	"fmt"
	"io"
)

const guideText = `Blocked region

Your network is in a region where Mozilla blocks part of the Firefox VPN
sign-in. Kumiho can still work. Pick one of these paths.

Option 1: I have another VPN (recommended)

  1. Turn on your other VPN.
  2. Sign in to Kumiho:        kumiho login
  3. Prepare Kumiho:           kumiho prepare
  4. When Kumiho says Prepared, turn off the other VPN.
  5. Connect:                  kumiho connect

  You need the other VPN again only if Kumiho stays off for more than
  10 minutes.

Option 2: Use a relay

  1. Deploy the relay from docs/relay.md, or use one you trust.
  2. Save its URL:             kumiho settings set relay_url https://relay.example
  3. Save its token:           kumiho settings set relay_token
  4. Test it:                  kumiho relay test
  5. Sign in with a password:  kumiho login
  6. Connect:                  kumiho connect

  Kumiho sends only blocked Mozilla requests through the relay. VPN traffic
  never goes through it.

Browser sign-in still needs another VPN or another device; the relay does
not proxy the browser sign-in page.

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
