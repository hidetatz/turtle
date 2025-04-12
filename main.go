package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"syscall"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

type mode int

const (
	normal mode = iota + 1
	insert
)

type cursorpos struct {
	x, y int // left, top is (0, 0)
}

type screen struct {
	rows   int
	cols   int
	mode   mode
	cursor cursorpos
	lines  [][]rune
}

type direction int

const (
	up direction = iota + 1
	down
	left
	right
)

func (s *screen) rendercursor() {
	movecursor(s.cursor.x, s.cursor.y)
}

func (s *screen) moveCursor(direction direction) bool {
	current := s.cursor

	switch direction {
	case up:
		if current.y == 0 {
			return false
		}

		s.cursor.y = current.y - 1 // move up

		// if upper line is shorter, the x should be the end of the upper line
		upperline := s.lines[s.cursor.y]
		if len(upperline) < current.x {
			s.cursor.x = len(upperline)
		}

		return true

	case down:
		if current.y == len(s.lines)-1 {
			return false
		}

		s.cursor.y = current.y + 1 // move down

		// if next line is shorter, the x should be the end of the next line
		nextline := s.lines[s.cursor.y]
		if len(nextline) < current.x {
			s.cursor.x = len(nextline)
		}

		return true

	case left:
		if current.x == 0 {
			return false
		}

		s.cursor.x -= 1

		return true

	case right:
		if current.x == len(s.lines[current.y]) {
			return false
		}

		s.cursor.x += 1

		return true

	default:
		panic("invalid direction is passed")
	}
}

func (s *screen) debug() {
	debug("cursor: {x: %v, y: %v}\n", s.cursor.x, s.cursor.y)
}

func main() {
	/*
	 * Prepare terminal
	 */

	var orig syscall.Termios
	if err := ioctl_getTermios(&orig); err != nil {
		panic(err)
	}

	// restore termios original setting at exit
	defer func() {
		if err := ioctl_setTermios(&orig); err != nil {
			panic(err)
		}
	}()

	conf := orig

	// see https://github.com/antirez/kilo/blob/master/kilo.c#L226-L239
	conf.Iflag &= ^(uint32(syscall.BRKINT) | uint32(syscall.ICRNL) | uint32(syscall.INPCK) | uint32(syscall.ISTRIP) | uint32(syscall.IXON))
	conf.Oflag &= ^(uint32(syscall.OPOST))
	conf.Cflag |= (uint32(syscall.CS8))
	conf.Lflag &= ^(uint32(syscall.ECHO) | uint32(syscall.ICANON) | uint32(syscall.IEXTEN) | uint32(syscall.ISIG))
	conf.Cc[syscall.VMIN] = 0
	conf.Cc[syscall.VTIME] = 1
	if err := ioctl_setTermios(&conf); err != nil {
		panic(err)
	}

	/*
	 * Prepare editor state
	 */

	row, col, _, _, err := ioctl_getWindowSize()
	if err != nil {
		panic(err)
	}

	s := &screen{
		rows:  row,
		cols:  col,
		lines: [][]rune{{}}, // initialize first line
		mode:  normal,       // normal mode on startup
	}

	refresh()

	cursorx, cursory := resetcursor()
	s.cursor.x = cursorx
	s.cursor.y = cursory

	/*
	 * start editor main routine
	 */

	reader := bufio.NewReader(os.Stdin)
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

		// s.debug()

		isArrowKey, dir := isarrowkey(b)
		var (
			cursormoved bool
		)

		switch s.mode {
		case normal:
			switch {
			case r == ctrl('q'):
				goto finish

			case r == 'i':
				s.mode = insert

			case r == 'h', isArrowKey && dir == left:
				cursormoved = s.moveCursor(left)

			case r == 'j', isArrowKey && dir == down:
				cursormoved = s.moveCursor(down)

			case r == 'k', isArrowKey && dir == up:
				cursormoved = s.moveCursor(up)

			case r == 'l', isArrowKey && dir == right:
				cursormoved = s.moveCursor(right)
			}

		case insert:
			switch {
			case r == 27: // Esc
				s.mode = normal

			case isArrowKey && dir == left:
				cursormoved = s.moveCursor(left)

			case isArrowKey && dir == down:
				cursormoved = s.moveCursor(down)

			case isArrowKey && dir == up:
				cursormoved = s.moveCursor(up)

			case isArrowKey && dir == right:
				cursormoved = s.moveCursor(right)

			case unicode.IsControl(r):
				debug("control key is pressed: %v\n", r)

			default:
				// edit the line on memory
				line := slices.Clone(s.lines[s.cursor.y])
				line = slices.Insert(line, s.cursor.x, r)
				s.lines[s.cursor.y] = line

				debug("%v, %v, %v\n", s.lines, s.cursor.x, s.cursor.y)

				// save current cursor
				curcursor := s.cursor
				curcursor.x += 1

				// render
				clearline()
				resetcursor()
				fmt.Fprint(os.Stdout, fmt.Sprintf("%s", string(line)))

				// restore cursor position
				s.cursor.x = curcursor.x
				s.cursor.y = curcursor.y
				cursormoved = true
			}

		default:
			panic("unknown mode")
		}

		if cursormoved {
			s.rendercursor()
		}
	}

finish:

	refresh()
	fmt.Fprintf(os.Stdout, "\n")
	resetcursor()
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

func refresh() {
	fmt.Fprint(os.Stdout, "\x1b[2J")
}

func clearline() {
	fmt.Fprint(os.Stdout, "\x1b[K")
}

func resetcursor() (int, int) {
	return movecursor(0, 0)
}

func movecursor(x, y int) (int, int) {
	fmt.Fprint(os.Stdout, fmt.Sprintf("\x1b[%v;%vH", y+1, x+1))
	return x, y
}

func ctrl(input byte) rune {
	return rune(input & 0x1f)
}

/*
 * ioctl stuff
 */

func ioctl_getWindowSize() (int, int, int, int, error) {
	type Winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	var w Winsize
	err := ioctl(syscall.Stdout, syscall.TIOCGWINSZ, unsafe.Pointer(&w))
	if err != nil {
		return 0, 0, 0, 0, err
	}

	return int(w.Row), int(w.Col), int(w.Xpixel), int(w.Ypixel), nil
}

func ioctl_getTermios(termios *syscall.Termios) error {
	if err := ioctl(syscall.Stdin, syscall.TCGETS, unsafe.Pointer(termios)); err != nil {
		return err
	}
	return nil
}

func ioctl_setTermios(termios *syscall.Termios) error {
	var TCSETSF uint = 0x5404 // not sure why but not defined in syscall package

	if err := ioctl(syscall.Stdin, TCSETSF, unsafe.Pointer(termios)); err != nil {
		return err
	}
	return nil
}

func ioctl(fd int, op uint, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(op), uintptr(arg))
	if errno != 0 {
		var err error
		// https://www.man7.org/linux/man-pages/man2/ioctl.2.html#ERRORS
		switch errno {
		case syscall.EBADF:
			err = fmt.Errorf("fd is not a valid")
		case syscall.EFAULT:
			err = fmt.Errorf("arg fault")
		case syscall.EINVAL:
			err = fmt.Errorf("invalid op or arg")
		case syscall.ENOTTY:
			err = fmt.Errorf("fd is not a character special dev")
		}
		return err
	}
	return nil
}
