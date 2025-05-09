turtle editor

## usage

### build

```shell
go build -o tt main.go
```

### run

```shell
# run with empty buffer
./tt

# open and edit a file
./tt test.txt
```

## keymaps

By default, turtle editor is in normal mode.

### normal mode

* `i`: enter insert mode
* `h`: move left
* `j`: move down
* `k`: move up
* `l`: move right
* `o`: insert a line **below** the current cursor, then enter insert mode
* `O`: insert a line **above** the current cursor, then enter insert mode
* `d`: delete a character

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
TURTLE_DEBUG=1 ./tt test.txt 2>log.txt

# on another terminal, tail it
tail -f log.txt
```
