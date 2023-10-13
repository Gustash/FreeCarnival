use std::path::PathBuf;

use clap::{Args, Parser, Subcommand, ValueEnum};

use crate::{constants::*, shared::models::api::BuildOs};

/// Native cross-platform indieGala client
#[derive(Parser, Debug)]
#[command(
    author,
    version = *HELP_VERSION,
    about,
    long_about = "FreeCarnival is a native and cross-platform CLI program to install and launch IndieGala games"
)]
pub(crate) struct Cli {
    #[command(subcommand)]
    pub(crate) command: Commands,
}

impl Cli {
    /// Checks if a sync is needed before handling command
    pub(crate) fn needs_sync(&self) -> bool {
        !matches!(
            &self.command,
            Commands::Login {
                email: _,
                password: _,
            } | Commands::Logout
                | Commands::Uninstall { slug: _, keep: _ }
                | Commands::Verify { slug: _ }
        )
    }
}

#[derive(Debug, Subcommand)]
pub(crate) enum Commands {
    /// Authenticate with your indieGala account
    Login {
        /// Your indieGala account email
        email: String,
        /// Your indieGala password, can be left blank for interactive login
        password: Option<String>,
    },
    /// Logout from your indieGala account
    Logout,
    /// List your library
    Library,
    /// Install a game from your library
    Install {
        /// The slug of the game e.g. syberia-ii
        slug: String,
        /// Install specific build version. If ommited, the latest build version will be installed.
        #[arg(long, short)]
        version: Option<String>,
        /// Base install path. The game will be installed in a subdirectory with the game's slugged
        /// name.
        #[arg(long)]
        base_path: Option<PathBuf>,
        /// Exact install path. The game will be installed in the selected directory without
        /// creating additional subdirectories.
        #[arg(long)]
        path: Option<PathBuf>,
        /// The build target OS to install
        #[arg(long)]
        os: Option<BuildOs>,
        #[command(flatten)]
        install_opts: InstallOpts,
    },
    /// Uninstalls a game
    Uninstall {
        /// The slug of the game e.g. syberia-ii
        slug: String,
        /// Remove game from installed config but do not delete install folder.
        #[arg(long)]
        keep: bool,
    },
    /// Lists available updates for installed games.
    ListUpdates,
    /// Update (or downgrade) an installed game.
    Update {
        /// The slug of the game e.g. syberia-ii
        slug: String,
        /// Change to a specific version. Don't set this if you just want to update to the latest
        /// version.
        ///
        /// You can get a list of available versions by using the `info` command.
        #[arg(long, short)]
        version: Option<String>,
        #[command(flatten)]
        install_opts: InstallOpts,
    },
    /// Launch an installed game
    Launch {
        /// The slug of the game e.g. syberia-ii
        slug: String,
        /// The WINE prefix to use for this game
        #[cfg(not(target_os = "windows"))]
        #[arg(long)]
        wine_prefix: Option<PathBuf>,
        /// The WINE bin to use for launching the game
        #[cfg(not(target_os = "windows"))]
        #[arg(long)]
        wine_bin: Option<PathBuf>,
    },
    /// Print info about game
    Info {
        /// The slug of the game e.g. syberia-ii
        slug: String,
    },
    /// Verify file integrity for an installed game
    Verify {
        /// The slug of the game e.g. syberia-ii
        slug: String,
    },
}

#[derive(Debug, Args)]
pub(crate) struct InstallOpts {
    /// How many download workers to run at one time.
    /// Increasing this value will make downloads faster, but use more memory.
    /// Lowering this value will lower memory usage at the cost of slower downloads.
    ///
    /// Note: Too many download workers can cause unreliable downloads. The default is
    /// double your CPU_COUNT. You shouldn't deviate too much from this.
    #[arg(long, default_value_t = *DEFAULT_MAX_DL_WORKERS)]
    pub(crate) max_download_workers: usize,
    /// How much memory to use to store chunks. Lowering this value will potentially make
    /// downloads slower while being lighter on memory usage. Raising it will make the program
    /// use more memory if needed, but can potentially speed up downloads.
    #[arg(long, default_value_t = *DEFAULT_MAX_MEMORY_USAGE)]
    pub(crate) max_memory_usage: usize,
    /// Print download info instead of installing game.
    #[arg(long, short)]
    pub(crate) info: bool,
    /// Skip verifying chunks. This will make downloads faster but won't check for
    /// corrupted/tampered files.
    #[arg(long)]
    pub(crate) skip_verify: bool,
}

impl ValueEnum for BuildOs {
    fn value_variants<'a>() -> &'a [Self] {
        &[Self::Windows, Self::Mac, Self::Linux]
    }

    fn to_possible_value(&self) -> Option<clap::builder::PossibleValue> {
        match self {
            Self::Windows => Some(clap::builder::PossibleValue::new("windows")),
            Self::Mac => {
                let possible_value = clap::builder::PossibleValue::new("mac");
                #[cfg(not(target_os = "macos"))]
                let possible_value = possible_value
                    .help("You can install macOS games, but you won't be able to run them!");

                Some(possible_value)
            }
            Self::Linux => {
                let possible_value = clap::builder::PossibleValue::new("linux");
                #[cfg(not(target_os = "linux"))]
                let possible_value = possible_value.help(
                    "You can install Linux games, but you probably won't be able to run them!",
                );

                Some(possible_value)
            }
        }
    }
}
