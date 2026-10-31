package serverlist

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Context is the Remote Settings JEXL environment.
type Context struct {
	FirefoxVersion string // "158.0"; empty when unknown
	Country        string // ISO 3166-1 alpha-2; empty when unknown
}

// Options controls filtering at parse time.
type Options struct {
	Context     Context
	AllowLocked bool
	Logf        func(format string, args ...any) // may be nil
}

// versionPattern matches the Firefox version forms the live server list uses:
// dotted numeric components with an optional a/b/rc suffix and number.
var versionPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)(?:(a|b|rc)([0-9]*))?$`)

type parsedVersion struct {
	numbers      []int
	suffix       int // 0 = a, 1 = b, 2 = rc, 3 = release
	suffixNumber int
}

func parseVersion(raw string) (parsedVersion, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	m := versionPattern.FindStringSubmatch(raw)
	if m == nil {
		return parsedVersion{}, false
	}
	parts := strings.Split(m[1], ".")
	numbers := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return parsedVersion{}, false
		}
		numbers[i] = n
	}
	pv := parsedVersion{numbers: numbers, suffix: 3}
	switch m[2] {
	case "a":
		pv.suffix = 0
	case "b":
		pv.suffix = 1
	case "rc":
		pv.suffix = 2
	}
	if m[3] != "" {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return parsedVersion{}, false
		}
		pv.suffixNumber = n
	}
	return pv, true
}

// compareVersionStrings returns -1, 0 or 1 for the Firefox ordering of a and
// b, or an error when either string is not a supported version.
func compareVersionStrings(a, b string) (int, error) {
	av, ok := parseVersion(a)
	if !ok {
		return 0, fmt.Errorf("invalid Firefox version %q", a)
	}
	bv, ok := parseVersion(b)
	if !ok {
		return 0, fmt.Errorf("invalid Firefox version %q", b)
	}
	if n := compareVersionNumbers(av, bv); n != 0 {
		return n, nil
	}
	if av.suffix != bv.suffix {
		if av.suffix < bv.suffix {
			return -1, nil
		}
		return 1, nil
	}
	switch {
	case av.suffixNumber < bv.suffixNumber:
		return -1, nil
	case av.suffixNumber > bv.suffixNumber:
		return 1, nil
	default:
		return 0, nil
	}
}

func compareVersionNumbers(a, b parsedVersion) int {
	n := len(a.numbers)
	if len(b.numbers) > n {
		n = len(b.numbers)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a.numbers) {
			av = a.numbers[i]
		}
		if i < len(b.numbers) {
			bv = b.numbers[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// compareVersions implements Firefox ordering. Invalid versions compare as
// equal; callers that need fail-open behaviour use compareVersionStrings.
func compareVersions(a, b string) int {
	n, err := compareVersionStrings(a, b)
	if err != nil {
		return 0
	}
	return n
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokBool
	tokInt
	tokString
	tokIdent
	tokLParen
	tokRParen
	tokPipe
	tokNot
	tokAnd
	tokOr
	tokEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
)

type token struct {
	kind tokenKind
	text string
	b    bool
	n    int
	pos  int
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.pos++
		default:
			goto scan
		}
	}
	return token{kind: tokEOF, pos: l.pos}, nil

scan:
	start := l.pos
	c := l.src[l.pos]
	switch c {
	case '(':
		l.pos++
		return token{kind: tokLParen, pos: start}, nil
	case ')':
		l.pos++
		return token{kind: tokRParen, pos: start}, nil
	case '!':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.pos += 2
			return token{kind: tokNe, pos: start}, nil
		}
		l.pos++
		return token{kind: tokNot, pos: start}, nil
	case '&':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '&' {
			l.pos += 2
			return token{kind: tokAnd, pos: start}, nil
		}
		return token{}, fmt.Errorf("unexpected %q at %d", c, start)
	case '|':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '|' {
			l.pos += 2
			return token{kind: tokOr, pos: start}, nil
		}
		l.pos++
		return token{kind: tokPipe, pos: start}, nil
	case '=':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.pos += 2
			return token{kind: tokEq, pos: start}, nil
		}
		return token{}, fmt.Errorf("unexpected %q at %d", c, start)
	case '<':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.pos += 2
			return token{kind: tokLe, pos: start}, nil
		}
		l.pos++
		return token{kind: tokLt, pos: start}, nil
	case '>':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.pos += 2
			return token{kind: tokGe, pos: start}, nil
		}
		l.pos++
		return token{kind: tokGt, pos: start}, nil
	case '\'', '"':
		quote := c
		l.pos++
		var b strings.Builder
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch == quote {
				l.pos++
				return token{kind: tokString, text: b.String(), pos: start}, nil
			}
			if ch == '\\' {
				l.pos++
				if l.pos >= len(l.src) {
					return token{}, fmt.Errorf("unterminated escape at %d", start)
				}
				switch l.src[l.pos] {
				case '\\', '\'', '"':
					b.WriteByte(l.src[l.pos])
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					return token{}, fmt.Errorf("unknown escape %q at %d", l.src[l.pos], l.pos)
				}
				l.pos++
				continue
			}
			b.WriteByte(ch)
			l.pos++
		}
		return token{}, fmt.Errorf("unterminated string at %d", start)
	}

	if c == '-' || (c >= '0' && c <= '9') {
		if c == '-' {
			l.pos++
			if l.pos >= len(l.src) || l.src[l.pos] < '0' || l.src[l.pos] > '9' {
				return token{}, fmt.Errorf("unexpected %q at %d", c, start)
			}
		}
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		n, err := strconv.Atoi(l.src[start:l.pos])
		if err != nil {
			return token{}, fmt.Errorf("invalid integer at %d: %v", start, err)
		}
		return token{kind: tokInt, n: n, pos: start}, nil
	}

	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
		l.pos++
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '_' || ch == '.' {
				l.pos++
				continue
			}
			break
		}
		text := l.src[start:l.pos]
		switch text {
		case "true":
			return token{kind: tokBool, b: true, text: text, pos: start}, nil
		case "false":
			return token{kind: tokBool, b: false, text: text, pos: start}, nil
		default:
			return token{kind: tokIdent, text: text, pos: start}, nil
		}
	}

	return token{}, fmt.Errorf("unexpected %q at %d", c, start)
}

type valueKind int

const (
	valueBool valueKind = iota
	valueString
	valueInt
)

func (k valueKind) String() string {
	switch k {
	case valueBool:
		return "boolean"
	case valueString:
		return "string"
	case valueInt:
		return "integer"
	default:
		return "unknown"
	}
}

type value struct {
	kind valueKind
	b    bool
	s    string
	n    int
}

func boolValue(b bool) value     { return value{kind: valueBool, b: b} }
func stringValue(s string) value { return value{kind: valueString, s: s} }
func intValue(n int) value       { return value{kind: valueInt, n: n} }

type parser struct {
	lx  *lexer
	tok token
	ctx Context
}

func newParser(expr string, ctx Context) (*parser, error) {
	p := &parser{lx: &lexer{src: expr}, ctx: ctx}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) advance() error {
	t, err := p.lx.next()
	if err != nil {
		return err
	}
	p.tok = t
	return nil
}

func (p *parser) parseOr() (value, error) {
	left, err := p.parseAnd()
	if err != nil {
		return value{}, err
	}
	for p.tok.kind == tokOr {
		if err := p.advance(); err != nil {
			return value{}, err
		}
		right, err := p.parseAnd()
		if err != nil {
			return value{}, err
		}
		lb, err := asBool(left)
		if err != nil {
			return value{}, err
		}
		rb, err := asBool(right)
		if err != nil {
			return value{}, err
		}
		left = boolValue(lb || rb)
	}
	return left, nil
}

func (p *parser) parseAnd() (value, error) {
	left, err := p.parseComparison()
	if err != nil {
		return value{}, err
	}
	for p.tok.kind == tokAnd {
		if err := p.advance(); err != nil {
			return value{}, err
		}
		right, err := p.parseComparison()
		if err != nil {
			return value{}, err
		}
		lb, err := asBool(left)
		if err != nil {
			return value{}, err
		}
		rb, err := asBool(right)
		if err != nil {
			return value{}, err
		}
		left = boolValue(lb && rb)
	}
	return left, nil
}

func (p *parser) parseComparison() (value, error) {
	left, err := p.parseUnary()
	if err != nil {
		return value{}, err
	}
	if !isComparison(p.tok.kind) {
		return left, nil
	}
	op := p.tok.kind
	if err := p.advance(); err != nil {
		return value{}, err
	}
	right, err := p.parseUnary()
	if err != nil {
		return value{}, err
	}
	return compareValues(left, right, op)
}

func isComparison(k tokenKind) bool {
	switch k {
	case tokEq, tokNe, tokLt, tokLe, tokGt, tokGe:
		return true
	default:
		return false
	}
}

func (p *parser) parseUnary() (value, error) {
	if p.tok.kind == tokNot {
		if err := p.advance(); err != nil {
			return value{}, err
		}
		v, err := p.parseUnary()
		if err != nil {
			return value{}, err
		}
		b, err := asBool(v)
		if err != nil {
			return value{}, err
		}
		return boolValue(!b), nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (value, error) {
	switch p.tok.kind {
	case tokLParen:
		if err := p.advance(); err != nil {
			return value{}, err
		}
		v, err := p.parseOr()
		if err != nil {
			return value{}, err
		}
		if p.tok.kind != tokRParen {
			return value{}, fmt.Errorf("expected ')' at %d", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return value{}, err
		}
		return v, nil
	case tokBool:
		v := boolValue(p.tok.b)
		if err := p.advance(); err != nil {
			return value{}, err
		}
		return v, nil
	case tokInt:
		v := intValue(p.tok.n)
		if err := p.advance(); err != nil {
			return value{}, err
		}
		return v, nil
	case tokString:
		v := stringValue(p.tok.text)
		if err := p.advance(); err != nil {
			return value{}, err
		}
		return v, nil
	case tokIdent:
		return p.parseReference()
	default:
		return value{}, fmt.Errorf("unexpected token at %d", p.tok.pos)
	}
}

func (p *parser) parseReference() (value, error) {
	ident := p.tok.text
	if err := p.advance(); err != nil {
		return value{}, err
	}
	var out value
	switch ident {
	case "env.version":
		if p.ctx.FirefoxVersion == "" {
			return value{}, fmt.Errorf("env.version is unavailable")
		}
		out = stringValue(p.ctx.FirefoxVersion)
	case "env.country":
		if p.ctx.Country == "" {
			return value{}, fmt.Errorf("env.country is unavailable")
		}
		out = stringValue(p.ctx.Country)
	default:
		return value{}, fmt.Errorf("unknown identifier %q", ident)
	}

	if p.tok.kind != tokPipe {
		return out, nil
	}
	if ident != "env.version" {
		return value{}, fmt.Errorf("transform is only supported on env.version")
	}
	if err := p.advance(); err != nil {
		return value{}, err
	}
	if p.tok.kind != tokIdent || p.tok.text != "versionCompare" {
		return value{}, fmt.Errorf("unknown transform %q", p.tok.text)
	}
	if err := p.advance(); err != nil {
		return value{}, err
	}
	if p.tok.kind != tokLParen {
		return value{}, fmt.Errorf("expected '(' after versionCompare")
	}
	if err := p.advance(); err != nil {
		return value{}, err
	}
	if p.tok.kind != tokString {
		return value{}, fmt.Errorf("versionCompare requires a string literal")
	}
	target := p.tok.text
	if err := p.advance(); err != nil {
		return value{}, err
	}
	if p.tok.kind != tokRParen {
		return value{}, fmt.Errorf("expected ')' after versionCompare argument")
	}
	if err := p.advance(); err != nil {
		return value{}, err
	}
	n, err := compareVersionStrings(p.ctx.FirefoxVersion, target)
	if err != nil {
		return value{}, err
	}
	return intValue(n), nil
}

func asBool(v value) (bool, error) {
	if v.kind != valueBool {
		return false, fmt.Errorf("expected boolean, got %s", v.kind)
	}
	return v.b, nil
}

func compareValues(a, b value, op tokenKind) (value, error) {
	if a.kind != b.kind {
		return value{}, fmt.Errorf("cannot compare %s with %s", a.kind, b.kind)
	}
	switch op {
	case tokEq:
		switch a.kind {
		case valueBool:
			return boolValue(a.b == b.b), nil
		case valueString:
			return boolValue(a.s == b.s), nil
		case valueInt:
			return boolValue(a.n == b.n), nil
		}
	case tokNe:
		switch a.kind {
		case valueBool:
			return boolValue(a.b != b.b), nil
		case valueString:
			return boolValue(a.s != b.s), nil
		case valueInt:
			return boolValue(a.n != b.n), nil
		}
	case tokLt, tokLe, tokGt, tokGe:
		if a.kind != valueInt {
			return value{}, fmt.Errorf("ordering requires integers, got %s", a.kind)
		}
		switch op {
		case tokLt:
			return boolValue(a.n < b.n), nil
		case tokLe:
			return boolValue(a.n <= b.n), nil
		case tokGt:
			return boolValue(a.n > b.n), nil
		case tokGe:
			return boolValue(a.n >= b.n), nil
		}
	}
	return value{}, fmt.Errorf("unsupported comparison")
}

// evalFilter evaluates one Remote Settings JEXL expression. The subset is
// deliberately small; any parse or evaluation error is returned so the caller
// can fail open.
func evalFilter(expr string, ctx Context) (bool, error) {
	p, err := newParser(expr, ctx)
	if err != nil {
		return false, err
	}
	v, err := p.parseOr()
	if err != nil {
		return false, err
	}
	if p.tok.kind != tokEOF {
		return false, fmt.Errorf("unexpected token at %d", p.tok.pos)
	}
	b, err := asBool(v)
	if err != nil {
		return false, err
	}
	return b, nil
}
