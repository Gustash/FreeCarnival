# FreeCarnival

FreeCarnival is a free and open-source CLI for managing your IndieGala library.

It supports installing, updating and launching games either natively, or through Wine on Linux/macOS.

## Usage

You can use the `freecarnival --help` command to get a helpful help document of the available commands.

Each command additionally supports a `--help` flag to display help documents about that specific command.

```
Usage: freecarnival <COMMAND>

Commands:
  login         Authenticate with your IndieGala account
  logout        Logout from your IndieGala account
  library       List your library (alias: sync)
  install       Install a game from your library
  uninstall     Uninstalls a game
  list-updates  Lists available updates for installed games (alias: updates)
  launch        Launch an installed game
  info          Print info about game
  verify        Verify file integrity for an installed game
  help          Print this message or the help of the given subcommand(s)

Options:
  -h, --help    Print help
```

## Building

Make sure you have Go 1.21+ installed on your system before building.

```bash
cd freecarnival-go && go build -o freecarnival
```

You can also run directly through `go run`:

```bash
go run . -- ARGS
```

## Features

- **Cross-platform**: Works on Windows, macOS, and Linux
- **Parallel downloads**: Configurable worker count for fast downloads
- **Memory management**: Configurable memory limits for chunk buffering
- **Delta updates**: Only downloads changed files when updating
- **Pause/Resume**: Automatically resume interrupted downloads
- **Wine support**: Automatically launches Windows games via Wine on macOS/Linux
- **Retry logic**: Automatic retry with exponential backoff for transient network errors
- **File verification**: SHA256 verification of downloaded chunks and installed files
- **Structured logging**: Configurable log levels (debug, info, warn, error)
