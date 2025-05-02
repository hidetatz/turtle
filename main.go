package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

var _debug bool

/*
 * mode
 */

type mode int

const (
	normal mode = iota + 1
	insert
)

func (m mode) String() string {
	switch m {
	case normal:
		return "NOR"
	case insert:
		return "INS"
	default:
		panic("unknown mode")
	}
}

/*
 * character
 */

// character represents a single character, such as "a", "1", "#", space, tab, etc.
type character struct {
	r     rune
	tab   bool // true if the character represents Tab.
	nl    bool // true if the character represents new line.
	width int
	str   string
}

func newCharacter(r rune) *character {
	// Raw Tab changes its size dynamically and it's hard to properly display, so
	// Tab is treated as 4 spaces.
	if r == '\t' {
		return &character{tab: true, width: 4, str: "    "}
	}

	// In turtle newline is rendered as a single space.
	if r == '\n' {
		return &character{nl: true, width: 1, str: " "}
	}

	if fullwidth(r) {
		return &character{r: r, width: 2, str: string(r)}
	}

	return &character{r: r, width: 1, str: string(r)}
}

func (c *character) copy() *character {
	return &character{c.r, c.tab, c.nl, c.width, c.str}
}

func (c *character) String() string {
	return c.str
}

/*
 * line
 */

type line struct {
	buffer []*character
}

func newemptyline() *line {
	return &line{buffer: []*character{newCharacter('\n')}}
}

func newline(s string) *line {
	runes := []rune(s)
	buff := make([]*character, len(runes))
	for i := range runes {
		buff[i] = newCharacter(runes[i])
	}
	buff = append(buff, newCharacter('\n'))
	return &line{buffer: buff}
}

func (l *line) String() string {
	// todo: can be cached
	sb := strings.Builder{}
	for _, c := range l.buffer {
		sb.WriteString(c.String())
	}
	return sb.String()
}

func (l *line) charidx(cursor, offset int) int {
	x := -offset
	for i, c := range l.buffer {
		x += c.width
		if x >= cursor+1 {
			return i
		}
	}

	panic("must not happen")
}

func (l *line) length() int {
	return len(l.buffer)
}

func (l *line) width() int {
	// todo: can be cached
	x := 0
	for i := range l.length() {
		x += l.buffer[i].width
	}
	return x
}

func (l *line) widthto(idx int) int {
	x := 0
	for i := range idx {
		x += l.buffer[i].width
	}
	return x
}

func (l *line) copy() *line {
	copy := &line{buffer: make([]*character, len(l.buffer))}
	for i := range l.buffer {
		copy.buffer[i] = l.buffer[i].copy()
	}
	return copy
}

func (l *line) insertchar(c *character, at int) {
	l.buffer = slices.Insert(l.buffer, at, c)
}

func (l *line) deletechar(at int) {
	l.buffer = slices.Delete(l.buffer, at, at+1)
}

func (l *line) cut(from, limit int) string {
	s := l.String()
	// todo: consider containing fullwidth char case
	length := len(s)

	if length < from {
		return ""
	}

	to := min(length, from+limit)

	return s[from:to]
}

/*
 * screen
 */

type screen struct {
	term    terminal
	height  int
	width   int
	mode    mode
	lines   []*line
	x       int // absolute x cursor based on term screen
	y       int // absolute y cursor based on buffer
	xoffset int
	yoffset int

	actualx int

	modechanged  bool
	scrolled     bool
	changedlines []int
}

func (s *screen) changeMode(mode mode) {
	s.mode = mode
	s.modechanged = true
}

func (s *screen) curline() *line {
	return s.lines[s.y]
}

func (s *screen) statusline() *line {
	return newline(fmt.Sprintf("mode: %v", s.mode))
}

func (s *screen) render() {
	/*
	 * After modifying the screen state, renders it to the terminal screen to synchronize everything.
	 */

	/* update status line */
	if s.modechanged {
		s.term.putcursor(0, s.height)
		s.term.clearline()
		fmt.Fprint(s.term, s.statusline().cut(0, s.width))
	}

	/* update texts */
	if s.scrolled {
		// update all lines
		for i := range s.height {
			s.term.putcursor(0, i)
			s.term.clearline()
			if s.yoffset+i < len(s.lines) {
				line := s.lines[s.yoffset+i]
				fmt.Fprint(s.term, line.cut(s.xoffset, s.width))
			}
		}
	} else if len(s.changedlines) != 0 {
		// udpate only changed lines
		slices.Sort(s.changedlines)
		s.changedlines = slices.Compact(s.changedlines)
		for _, l := range s.changedlines {
			// if changed line is not shown on the screen, skip
			if l < s.yoffset || s.height-1 < l-s.yoffset {
				continue
			}

			s.term.putcursor(0, l-s.yoffset)
			s.term.clearline()

			if l <= len(s.lines)-1 {
				line := s.lines[l].cut(s.xoffset, s.width)
				fmt.Fprint(s.term, line)
			}
		}
	}

	/* update cursor position */

	// NOTE: some tweaking x is applied here, but this does not change s.x.
	curline := s.curline()
	width := curline.width()
	x := s.x - s.xoffset

	// when the x is too right, set x to the line tail.
	// This must not change s.x because s.x should be kept when moving to another long line.
	if (width - 1) < s.x {
		x = (width - 1) - s.xoffset
	}

	// set x to be head of the character. this works when the current char width is not 1.
	// This must not change s.x because s.x should be kept when moving to another line.
	curidx := curline.charidx(x, s.xoffset)
	x = curline.widthto(curidx) - s.xoffset

	// move cursor after all.
	s.term.putcursor(x, s.y-s.yoffset)
	s.actualx = x

	s.modechanged = false
	s.scrolled = false
	s.changedlines = []int{}
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (s *screen) movecursor(direction direction, cnt int) {
	/*
	 * 1. move x/y.
	 * note that x and y is absolute coordination in the whole text buffer, so
	 * after this switch statement, x and y might be pointing somewhere out the
	 * terminal screen.
	 */
	switch direction {
	case up:
		s.y = max(s.y-cnt, 0)

	case down:
		s.y = min(s.y+cnt, len(s.lines)-1)

	case left:
		curline := s.curline()
		curidx := s.curCharIdxX()
		s.x = curline.widthto(max(curidx-cnt, 0))

	case right:
		curline := s.curline()
		curidx := s.curCharIdxX()
		s.x = curline.widthto(min(curidx+cnt, curline.length()-1))

	default:
		panic("invalid direction is passed")
	}

	/*
	 * As of here, the x and y might be pointing out the screen,
	 * 2. if so, do scroll.
	 */

	/*
	 * horizontal scroll
	 */

	pad := 4

	switch {
	case max(0, s.x-pad) < s.xoffset:
		delta := s.xoffset - max(0, s.x-pad)
		s.xoffset -= delta
		s.scrolled = true

	case s.xoffset+s.width-1 < min(s.curline().width(), s.x+pad):
		delta := min(s.curline().width(), s.x+pad) - (s.xoffset + s.width - 1)
		s.xoffset += delta
		s.scrolled = true
	}

	/*
	 * vertical scroll
	 */
	switch {
	case max(0, s.y-pad) < s.yoffset:
		// scroll up
		delta := s.yoffset - max(0, s.y-pad)
		s.yoffset -= delta
		s.scrolled = true

	case s.yoffset+s.height-1 < min(len(s.lines), s.y+pad):
		// scroll down
		delta := min(len(s.lines), s.y+pad) - (s.yoffset + s.height - 1)
		s.yoffset += delta
		s.scrolled = true
	}

	/*
	 * 3. After the cursor move and scroll, if the current line is not shown as
	 * it's too short, scroll left.
	 */
	width := s.curline().width()
	if width-1 < s.xoffset {
		delta := s.xoffset - (width - 1)
		s.xoffset -= delta
		s.scrolled = true
	}
}

func (s *screen) insertline(direction direction) {
	switch direction {
	case up:
		s.lines = slices.Insert(s.lines, s.y, newemptyline())
	case down:
		s.lines = slices.Insert(s.lines, s.y+1, newemptyline())
	default:
		panic("invalid direction is passed to addline")
	}

	for i := s.y; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}
}

func (s *screen) insertchar(c *character) {
	var idx int
	if s.curline().length() == 0 {
		idx = 0
	} else {
		idx = s.curCharIdxX()
	}
	s.curline().insertchar(c, idx)
	s.changedlines = append(s.changedlines, s.y)
}

func (s *screen) deleteCurrentChar() {
	s.curline().deletechar(s.curCharIdxX())
	s.changedlines = append(s.changedlines, s.y)
}

func (s *screen) curCharIdxX() int {
	return s.curline().charidx(s.actualx, s.xoffset)
}

func (s *screen) deleteline(y int) {
	for i := y; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}
	s.lines = slices.Delete(s.lines, y, y+1)
}

func (s *screen) moveXToRightEdgeCharIfNecessary() bool {
	curline := s.curline()
	width := curline.width()

	// if the cursor is pointing the place on which no character exists ("too right"),
	// move cursor to the rightmost character first.
	if width-1 < s.x+s.xoffset {
		if width == 1 {
			s.xoffset = 0
			s.x = 0
			s.scrolled = true
		} else if 0 < width-s.xoffset {
			s.x = width - s.xoffset - 1
		} else {
			// show right edge character for useful
			s.xoffset = width - 2
			s.x = 1
			s.scrolled = true
		}
		return true
	}

	return false
}

// from, to both inclusive
func (s *screen) joinLines(from, to int) int {
	if to < from {
		from, to = to, from
	}

	if from <= 0 {
		from = 0
	} else if len(s.lines)-1 < from {
		from = len(s.lines)
	}

	if to <= 0 {
		to = 0
	} else if len(s.lines)-1 < to {
		to = len(s.lines)
	}

	if from == to {
		return from
	}

	base := s.lines[from]
	base.deletechar(len(base.buffer) - 1) // delete \n

	for i := from + 1; i <= to; i++ {
		l := s.lines[i]
		l.deletechar(len(l.buffer) - 1)
		base.buffer = append(base.buffer, l.buffer...)
	}

	base.buffer = append(base.buffer, newCharacter('\n')) // restore deleted \n

	for i := from + 1; i <= to; i++ {
		s.deleteline(i)
	}

	for i := from; i < len(s.lines); i++ {
		s.changedlines = append(s.changedlines, i)
	}

	return from
}

func (s *screen) calcx(idx int) int {
	curline := s.curline()
	x := 0
	for i := range idx {
		x += curline.buffer[i].width
	}
	if x < s.xoffset {
		return 0
	}

	return x - s.xoffset
}

func (s *screen) debug(msg ...string) {
	debug("msg: %v, height: %v, width: %v, mode: %v, x: %v, y: %v, actualx: %v, lines: %v, curlinelen: %v, curlinewidth: %v, xoffset: %v, yoffset: %v\n",
		msg, s.height, s.width, s.mode, s.x, s.y, s.actualx, len(s.lines), s.curline().length(), s.curline().width(), s.xoffset, s.yoffset)
}

func main() {
	dbg := os.Getenv("TURTLE_DEBUG")
	if dbg != "" {
		_debug = true
	}

	flag.Parse()
	args := flag.Args()

	var r io.Reader

	switch len(args) {
	case 0:
		r = strings.NewReader("")

	case 1:
		filename := args[0]
		f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		r = f

	default:
		fmt.Println("more than 2 args are passed")
		return
	}

	term := &unixVT100term{}
	editor(term, r, os.Stdin)
}

func editor(term terminal, text io.Reader, input io.Reader) {
	fin, err := term.init()
	if err != nil {
		fin()
		panic(err)
	}

	defer fin()
	defer term.refresh()

	term.refresh()

	/*
	 * Prepare editor state
	 */

	row, col, err := term.windowsize()
	if err != nil {
		panic(err)
	}

	s := &screen{
		term:        term,
		height:      row - 2,
		width:       col,
		mode:        normal,
		x:           0,
		y:           0,
		xoffset:     0,
		yoffset:     0,
		modechanged: true,
		scrolled:    true,
		lines:       []*line{},
	}

	scanner := bufio.NewScanner(text)
	for scanner.Scan() {
		line := scanner.Text()
		s.lines = append(s.lines, newline(line))
	}

	if len(s.lines) == 0 {
		s.lines = []*line{newemptyline()}
	}

	s.render()

	/*
	 * start editor main routine
	 */

	reader := bufio.NewReader(input)
	for {
		// a unicode character takes 4 bytes at most
		b := make([]byte, 4)
		n, err := reader.Read(b)
		if err == io.EOF {
			continue
		}

		if n == -1 {
			panic("cannot read")
		}

		if err != nil {
			panic(err)
		}

		r, _ := utf8.DecodeRune(b)

		isArrowKey, dir := isarrowkey(b)

		switch s.mode {
		case normal:
			switch {
			case r == ctrl('q'):
				goto finish

			case r == 'i':
				s.changeMode(insert)

			case r == 'd':
				s.moveXToRightEdgeCharIfNecessary()
				curline := s.curline()
				curidx := curline.charidx(s.x, s.xoffset)
				removingNL := curidx == curline.length()-1

				if removingNL {
					// if nl is removed, concat current and next line.
					s.joinLines(s.y, s.y+1)
				} else {
					s.deleteCurrentChar()
					s.x = s.calcx(curidx)
				}

			case r == 'o':
				s.insertline(down)
				s.movecursor(down, 1)
				s.changeMode(insert)

			case r == 'O':
				s.insertline(up)
				s.changeMode(insert)

			case r == 'h', isArrowKey && dir == left:
				s.movecursor(left, 1)

			case r == 'j', isArrowKey && dir == down:
				s.movecursor(down, 1)

			case r == 'k', isArrowKey && dir == up:
				s.movecursor(up, 1)

			case r == 'l', isArrowKey && dir == right:
				s.movecursor(right, 1)
			}

		case insert:
			switch {
			case r == 27: // Esc
				s.changeMode(normal)

			case isArrowKey && dir == left:
				s.movecursor(left, 1)

			case isArrowKey && dir == down:
				s.movecursor(down, 1)

			case isArrowKey && dir == up:
				s.movecursor(up, 1)

			case isArrowKey && dir == right:
				s.movecursor(right, 1)

			case r == 13: // Enter
				s.moveXToRightEdgeCharIfNecessary()

				curline := s.curline()
				copy := curline.copy()
				curidx := curline.charidx(s.x, s.xoffset)

				// cut current line
				s.lines[s.y].buffer = append(curline.buffer[:curidx], newCharacter('\n'))

				// insert line below
				s.insertline(down)
				s.lines[s.y+1].buffer = copy.buffer[curidx:]
				s.movecursor(down, 1)
				s.x = 0
				if s.xoffset != 0 {
					s.xoffset = 0
					s.scrolled = true
				}

			case r == 127: // Backspace
				s.moveXToRightEdgeCharIfNecessary()

				curline := s.curline()
				curidx := curline.charidx(s.x, s.xoffset)

				switch {
				case curidx == 0 && s.y == 0:
					// already at the top. do nothing

				case curidx == 0:
					abovelinelen := s.lines[s.y-1].length()
					s.joinLines(s.y, s.y-1)
					s.movecursor(up, 1)
					s.x = s.calcx(abovelinelen - 1)
					if s.width-1 < s.x {
						s.xoffset = s.x - s.width/2
						s.x = s.x - s.xoffset
						s.scrolled = true
					}

				default:
					// delete the char
					curline.deletechar(curidx - 1)
					s.x = s.calcx(curidx - 1)
					if s.x == 0 && s.xoffset != 0 {
						s.xoffset--
						s.x = 1
						s.scrolled = true
					}
					s.changedlines = append(s.changedlines, s.y)
				}

			case unicode.IsControl(r):
				if _debug {
					debug("control key is pressed: %v\n", r)
				}

			default:
				s.moveXToRightEdgeCharIfNecessary()
				s.insertchar(newCharacter(r))
				s.movecursor(right, 1)
			}

		default:
			panic("unknown mode")
		}

		s.render()

		if _debug {
			s.debug()
		}
	}

finish:
	term.refresh()
	fmt.Fprintf(os.Stdout, "\n")
	term.putcursor(0, 0)
}

func isarrowkey(bs []byte) (bool, direction) {
	if bs[0] != '\x1b' || bs[1] != '[' {
		return false, 0
	}

	switch bs[2] {
	case 'A':
		return true, up
	case 'B':
		return true, down
	case 'C':
		return true, right
	case 'D':
		return true, left

		// todo: handle other keys
	}

	panic("unknown key came as arrow key")
}

func debug(format string, a ...any) (int, error) {
	return fmt.Fprintf(os.Stderr, format, a...)
}

/*
 * terminal
 */

func ctrl(input byte) rune {
	return rune(input & 0x1f)
}

type terminal interface {
	io.Writer

	init() (func(), error)
	windowsize() (int, int, error)
	refresh()
	clearline()
	putcursor(x, y int)
}

type unixVT100term struct{}

func (t *unixVT100term) init() (func(), error) {
	oldstate, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return func() {}, err
	}
	return func() { term.Restore(int(os.Stdin.Fd()), oldstate) }, nil
}

func (t *unixVT100term) windowsize() (int, int, error) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	return height, width, err
}

func (t *unixVT100term) refresh() {
	fmt.Fprint(t, "\x1b[2J")
}

func (t *unixVT100term) clearline() {
	fmt.Fprint(t, "\x1b[K")
}

func (t *unixVT100term) putcursor(x, y int) {
	fmt.Fprint(t, fmt.Sprintf("\x1b[%v;%vH", y+1, x+1))
}

func (t *unixVT100term) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

/*
 * unicode utilities
 */

func fullwidth(r rune) bool {
	fullwidth := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x3000, 0x3000, 1},
			{0xff01, 0xff60, 1},
			{0xffe0, 0xffe6, 1},
		},
	}

	wide := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x1100, 0x115F, 1},
			{0x2329, 0x232A, 1},
			{0x2E80, 0x2FFB, 1},
			{0x3001, 0x303E, 1},
			{0x3041, 0x33FF, 1},
			{0x3400, 0x4DB5, 1},
			{0x4E00, 0x9FBB, 1},
			{0xA000, 0xA4C6, 1},
			{0xAC00, 0xD7A3, 1},
			{0xF900, 0xFAD9, 1},
			{0xFE10, 0xFE19, 1},
			{0xFE30, 0xFE6B, 1},
		},
		R32: []unicode.Range32{
			{0x20000, 0x2A6D6, 1},
			{0x2A6D7, 0x2F7FF, 1},
			{0x2F800, 0x2FA1D, 1},
			{0x2FA1E, 0x2FFFD, 1},
			{0x30000, 0x3FFFD, 1},
		},
	}

	ambiguous := &unicode.RangeTable{
		R16: []unicode.Range16{
			{0x00A1, 0x00A1, 1},
			{0x00A4, 0x00A4, 1},
			{0x00A7, 0x00A8, 1},
			{0x00AA, 0x00AA, 1},
			{0x00AD, 0x00AE, 1},
			{0x00B0, 0x00B4, 1},
			{0x00B6, 0x00BA, 1},
			{0x00BC, 0x00BF, 1},
			{0x00C6, 0x00C6, 1},
			{0x00D0, 0x00D0, 1},
			{0x00D7, 0x00D8, 1},
			{0x00DE, 0x00E1, 1},
			{0x00E6, 0x00E6, 1},
			{0x00E8, 0x00EA, 1},
			{0x00EC, 0x00ED, 1},
			{0x00F0, 0x00F0, 1},
			{0x00F2, 0x00F3, 1},
			{0x00F7, 0x00FA, 1},
			{0x00FC, 0x00FC, 1},
			{0x00FE, 0x00FE, 1},
			{0x0101, 0x0101, 1},
			{0x0111, 0x0111, 1},
			{0x0113, 0x0113, 1},
			{0x011B, 0x011B, 1},
			{0x0126, 0x0127, 1},
			{0x012B, 0x012B, 1},
			{0x0131, 0x0133, 1},
			{0x0138, 0x0138, 1},
			{0x013F, 0x0142, 1},
			{0x0144, 0x0144, 1},
			{0x0148, 0x014B, 1},
			{0x014D, 0x014D, 1},
			{0x0152, 0x0153, 1},
			{0x0166, 0x0167, 1},
			{0x016B, 0x016B, 1},
			{0x01CE, 0x01CE, 1},
			{0x01D0, 0x01D0, 1},
			{0x01D2, 0x01D2, 1},
			{0x01D4, 0x01D4, 1},
			{0x01D6, 0x01D6, 1},
			{0x01D8, 0x01D8, 1},
			{0x01DA, 0x01DA, 1},
			{0x01DC, 0x01DC, 1},
			{0x0251, 0x0251, 1},
			{0x0261, 0x0261, 1},
			{0x02C4, 0x02C4, 1},
			{0x02C7, 0x02C7, 1},
			{0x02C9, 0x02CB, 1},
			{0x02CD, 0x02CD, 1},
			{0x02D0, 0x02D0, 1},
			{0x02D8, 0x02DB, 1},
			{0x02DD, 0x02DD, 1},
			{0x02DF, 0x02DF, 1},
			{0x0300, 0x036F, 1},
			{0x0391, 0x03A9, 1},
			{0x03B1, 0x03C1, 1},
			{0x03C3, 0x03C9, 1},
			{0x0401, 0x0401, 1},
			{0x0410, 0x044F, 1},
			{0x0451, 0x0451, 1},
			{0x2010, 0x2010, 1},
			{0x2013, 0x2016, 1},
			{0x2018, 0x2019, 1},
			{0x201C, 0x201D, 1},
			{0x2020, 0x2022, 1},
			{0x2024, 0x2027, 1},
			{0x2030, 0x2030, 1},
			{0x2032, 0x2033, 1},
			{0x2035, 0x2035, 1},
			{0x203B, 0x203B, 1},
			{0x203E, 0x203E, 1},
			{0x2074, 0x2074, 1},
			{0x207F, 0x207F, 1},
			{0x2081, 0x2084, 1},
			{0x20AC, 0x20AC, 1},
			{0x2103, 0x2103, 1},
			{0x2105, 0x2105, 1},
			{0x2109, 0x2109, 1},
			{0x2113, 0x2113, 1},
			{0x2116, 0x2116, 1},
			{0x2121, 0x2122, 1},
			{0x2126, 0x2126, 1},
			{0x212B, 0x212B, 1},
			{0x2153, 0x2154, 1},
			{0x215B, 0x215E, 1},
			{0x2160, 0x216B, 1},
			{0x2170, 0x2179, 1},
			{0x2190, 0x2199, 1},
			{0x21B8, 0x21B9, 1},
			{0x21D2, 0x21D2, 1},
			{0x21D4, 0x21D4, 1},
			{0x21E7, 0x21E7, 1},
			{0x2200, 0x2200, 1},
			{0x2202, 0x2203, 1},
			{0x2207, 0x2208, 1},
			{0x220B, 0x220B, 1},
			{0x220F, 0x220F, 1},
			{0x2211, 0x2211, 1},
			{0x2215, 0x2215, 1},
			{0x221A, 0x221A, 1},
			{0x221D, 0x2220, 1},
			{0x2223, 0x2223, 1},
			{0x2225, 0x2225, 1},
			{0x2227, 0x222C, 1},
			{0x222E, 0x222E, 1},
			{0x2234, 0x2237, 1},
			{0x223C, 0x223D, 1},
			{0x2248, 0x2248, 1},
			{0x224C, 0x224C, 1},
			{0x2252, 0x2252, 1},
			{0x2260, 0x2261, 1},
			{0x2264, 0x2267, 1},
			{0x226A, 0x226B, 1},
			{0x226E, 0x226F, 1},
			{0x2282, 0x2283, 1},
			{0x2286, 0x2287, 1},
			{0x2295, 0x2295, 1},
			{0x2299, 0x2299, 1},
			{0x22A5, 0x22A5, 1},
			{0x22BF, 0x22BF, 1},
			{0x2312, 0x2312, 1},
			{0x2460, 0x24E9, 1},
			{0x24EB, 0x254B, 1},
			{0x2550, 0x2573, 1},
			{0x2580, 0x258F, 1},
			{0x2592, 0x2595, 1},
			{0x25A0, 0x25A1, 1},
			{0x25A3, 0x25A9, 1},
			{0x25B2, 0x25B3, 1},
			{0x25B6, 0x25B7, 1},
			{0x25BC, 0x25BD, 1},
			{0x25C0, 0x25C1, 1},
			{0x25C6, 0x25C8, 1},
			{0x25CB, 0x25CB, 1},
			{0x25CE, 0x25D1, 1},
			{0x25E2, 0x25E5, 1},
			{0x25EF, 0x25EF, 1},
			{0x2605, 0x2606, 1},
			{0x2609, 0x2609, 1},
			{0x260E, 0x260F, 1},
			{0x2614, 0x2615, 1},
			{0x261C, 0x261C, 1},
			{0x261E, 0x261E, 1},
			{0x2640, 0x2640, 1},
			{0x2642, 0x2642, 1},
			{0x2660, 0x2661, 1},
			{0x2663, 0x2665, 1},
			{0x2667, 0x266A, 1},
			{0x266C, 0x266D, 1},
			{0x266F, 0x266F, 1},
			{0x273D, 0x273D, 1},
			{0x2776, 0x277F, 1},
			{0xE000, 0xF8FF, 1},
			{0xFE00, 0xFE0F, 1},
			{0xFFFD, 0xFFFD, 1},
		},
		R32: []unicode.Range32{
			{0xE0100, 0xE01EF, 1},
			{0xF0000, 0xFFFFD, 1},
			{0x100000, 0x10FFFD, 1},
		},
	}

	return unicode.Is(fullwidth, r) || unicode.Is(wide, r) || unicode.Is(ambiguous, r)
}
