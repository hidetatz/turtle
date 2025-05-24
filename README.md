turtle editor - An old-fashioned cli text editor for unix.

![Image](https://github.com/user-attachments/assets/84280ddc-f619-41ec-a348-3412bbefb21e)

## usage

### build

```shell
go build -o tt main.go
```

### install

```shell
go build -o tt main.go && mv ./tt $(go env GOPATH)/bin/tt
```

### run

```shell
# run with empty buffer
tt

# open and edit a file
tt test.txt
```

## keymaps

By default, turtle editor is in normal mode.

### normal mode

For some commands, leading number before command executes the command n-times.
For example, `3j` moves the cursor down by 3 times.

* `i`: enter insert mode
* `:`: enter command mode
* `h`: move left
* `j`: move down
* `k`: move up
* `l`: move right
* `f <character>`: find and move to the **next** <character> on the current line
* `F <character>`: find and move to the **previous** <character> on the current line
* `o`: insert a line **below** the current cursor, then enter insert mode
* `O`: insert a line **above** the current cursor, then enter insert mode
* `d`: delete a character
* `<number> G`: move to the <number> line
* `gg`: move to the text head
* `ge`: move to the text bottom
* `gh`: move to the current line head
* `gl`: move to the current line tail
* `gs`: move to the current line head where non-space character exists
* `Ctrl-u`: scroll up by half page
* `Ctrl-d`: scroll down by half page
* `Ctrl-w` `h`: move to left window
* `Ctrl-w` `j`: move to below window
* `Ctrl-w` `k`: move to above window
* `Ctrl-w` `l`: move to right window

### command mode

Every command is executed after Enter keypress.

* `q`: close the current buffer
* `q!`: close the current buffer even if the unsaved change reminaing
* `w`: save the buffer
* `wq`: save and close the buffer
* `vs filename`: opens a new file in vertically split window
* `hs filename`: opens a new file in horizontally split window

### insert mode

In insert mode, you can edit the text.

* `Esc`: exit insert mode

## development

### run test

```shell
go test ./...
```

### debug

Logs put via debug() are written to stderr as stdout is used by the editor itself.

```shell
# run with stderr redirected to another file
TURTLE_DEBUG=1 tt README.md 2>log.txt

# on another terminal, tail it
tail -f log.txt
```
