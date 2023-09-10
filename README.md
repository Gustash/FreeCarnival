# FreeCarnival

FreeCarnival is a free and open-source CLI for managing your IndieGala library.

It supports installing, updating and launching games either natively, or through Wine on Linux/macOS.

## Usage

You can use the `freecarnival --help` command to get a helpful help document of the available commands.

Each command additionally supports a `--help` flag to display help documents about that specific command.

```
Usage: freecarnival <COMMAND>

Commands:
  login         Authenticate with your indieGala account
  logout        Logout from your indieGala account
  library       List your library
  install       Install a game from your library
  uninstall     Uninstalls a game
  list-updates  Lists available updates for installed games
  update        Update (or downgrade) an installed game
  launch        Launch an installed game
  info          Print info about game
  verify        Verify file integrity for an installed game
  help          Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version
```

## Building

Make sure you have Rust installed on your system before building.

```bash
$ cd FreeCarnival && cargo build --release
```

The compiled binary will be in `target/release`.

You can also run directly through Cargo when debugging:

```bash
$ cargo run -- ARGS
```

## v1 Roadmap

- [x] Authentication expiry refresh
- [ ] Logger
- [ ] Better download fail handling
- [ ] Pause/Resume download
- [x] Windows support
- [ ] Code Refactoring
- [ ] Better error handling
