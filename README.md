# cshell

https://github.com/user-attachments/assets/4165778e-a7d9-4fdb-a16e-82df460623e6

A POSIX-style shell written from scratch in Go, built around one idea: most
of the time, the argument you are about to type is already printed on your
screen. cshell captures everything commands print and lets you grab any of
it into the current command line with a keystroke.

## The grab feature

Press **Ctrl+G** while typing a command: a fuzzy picker opens over every
token from previous command output. Type to filter, arrows to choose, Enter
to insert it at the cursor. No more mouse-selecting a filename out of `ls`
output or retyping a container id.

This works because every command runs on a shell-owned pseudo-terminal (the
tmux model): commands see a real tty — colors, `isatty`, window size,
working `/dev/tty` for pagers and editors — while the shell mirrors all
output to your screen and into a scrollback buffer that feeds the picker.

## Everything else

- Hand-written lexer (POSIX quoting, escapes, fd redirects, `#` comments)
  and Pratt-style parser producing an AST
- Pipelines over real OS pipes, `&&` / `||` / `;`, redirects
  (`>`, `>>`, `<`, `2>`, `2>&1`), exit-status tracking
- Line editor built on raw mode: emacs keybindings, horizontal scrolling,
  PS2 continuation for unclosed quotes
- Persistent history with ↑/↓ and **Ctrl+R** reverse incremental search
- Tab completion: builtins and PATH commands for the first word, file
  paths elsewhere, `~` aware
- `~/.cshrc` startup file, shell variables, `export`, and PS1/PS2 prompts
  with bash-style escapes (`\u \h \w \W \$ \e ...`) including colors
- Tilde expansion in arguments and redirect targets, quote-aware

## Install

### Debian / Ubuntu — one-liner

```sh
curl -fsSL https://iam-abdul.github.io/cshell/install.sh | sudo sh
```

This sets up the signed apt repository and installs cshell. Future updates
then come through `sudo apt upgrade`.

### Debian / Ubuntu — manual apt setup

If you would rather add the repository yourself instead of piping a script:

```sh
curl -fsSL https://iam-abdul.github.io/cshell/key.gpg \
  | sudo gpg --dearmor -o /usr/share/keyrings/cshell.gpg
echo 'deb [signed-by=/usr/share/keyrings/cshell.gpg] https://iam-abdul.github.io/cshell stable main' \
  | sudo tee /etc/apt/sources.list.d/cshell.list
sudo apt update && sudo apt install cshell
```

### Prebuilt binary (Linux / macOS)

Grab the archive for your OS/arch from the
[latest release](https://github.com/iam-abdul/cshell/releases/latest),
extract it, and put `cshell` on your `PATH`.

## Run it

```sh
go run ./app
```

Piped input (`echo 'ls | wc -l' | cshell`) uses a plain non-interactive
loop, so it scripts fine too.

## Tests

```sh
go test ./app/
```

Unit tests cover the lexer, parser, executor, line-editor state machine,
history, completion, scrollback, and picker. Integration tests drive the
real compiled binary under a pseudo-terminal, typing keystrokes and
asserting on what the terminal shows — including opening and quitting
`man`, tab completion, Ctrl+R search, and the grab picker.

## Not there yet

`$VAR` expansion, globbing, job control (`&`, `fg`), heredocs, subshells.
