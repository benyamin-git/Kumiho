package serverlist

import "strings"

// isoFlagCodes is the bundled ISO 3166-1 alpha-2 set. FlagEmoji only emits
// flags for a real code; an unknown two-letter value would otherwise render
// as unrelated regional-indicator letters.
const isoFlagCodes = `
ad ae af ag ai al am ao aq ar as at au aw ax az ba bb bd be bf bg bh bi bj
bl bm bn bo bq br bs bt bv bw by bz ca cc cd cf cg ch ci ck cl cm cn co cr
cu cv cw cx cy cz de dj dk dm do dz ec ee eg eh er es et fi fj fk fm fo fr
ga gb gd ge gf gg gh gi gl gm gn gp gq gr gs gt gu gw gy hk hm hn hr ht hu
id ie il im in io iq ir is it je jm jo jp ke kg kh ki km kn kp kr kw ky kz
la lb lc li lk lr ls lt lu lv ly ma mc md me mf mg mh mk ml mm mn mo mp mq
mr ms mt mu mv mw mx my mz na nc ne nf ng ni nl no np nr nu nz om pa pe pf
pg ph pk pl pm pn pr ps pt pw py qa re ro rs ru rw sa sb sc sd se sg sh si
sj sk sl sm sn so sr ss st sv sx sy sz tc td tf tg th tj tk tl tm tn to tr
tt tv tw tz ua ug um us uy uz va vc ve vg vi vn vu wf ws ye yt za zm zw
`

var isoFlagSet = func() map[string]bool {
	set := make(map[string]bool, 249)
	for _, code := range strings.Fields(isoFlagCodes) {
		set[code] = true
	}
	return set
}()

// FlagEmoji returns the ISO 3166-1 alpha-2 flag emoji for code, or "" when the
// code is RecommendedCode, blank, not exactly two ASCII letters, or not in the
// bundled ISO set.
func FlagEmoji(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == RecommendedCode || len(code) != 2 || !isoFlagSet[strings.ToLower(code)] {
		return ""
	}
	for i := 0; i < 2; i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return ""
		}
	}
	const base = 0x1F1E6
	return string([]rune{
		rune(base + int(code[0]-'A')),
		rune(base + int(code[1]-'A')),
	})
}
