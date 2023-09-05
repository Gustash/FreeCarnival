use std::path::PathBuf;

use clap::{Parser, Subcommand};

use crate::constants::{DEFAULT_MAX_DL_WORKERS, DEFAULT_MAX_MEMORY_USAGE};

/// Native cross-platform indieGala client
#[derive(Parser, Debug)]
#[command(
    author,
    version,
    about,
    long_about = "openGala is a native and cross-platform CLI program to install and launch indieGala games"
)]
pub(crate) struct Cli {
    #[command(subcommand)]
    pub(crate) command: Commands,
}

#[derive(Debug, Subcommand)]
pub(crate) enum Commands {
    /// Authenticate with your indieGala account
    Login {
        /// Your indieGala username
        username: String,
        /// Your indieGala password, can be left blank for interactive login
        password: Option<String>,
    },
    /// Logout from your indieGala account
    Logout,
    /// Sync user info and library
    Sync,
    /// List your library
    Library,
    /// Install a game from your library
    Install {
        slug: String,
        /// How many download workers to run at one time.
        /// Increasing this value will make downloads faster, but use more memory.
        /// Lowering this value will lower memory usage at the cost of slower downloads.
        ///
        /// Note: Too many download workers can cause unreliable downloads. The default is
        /// double your CPU_COUNT. You shouldn't deviate too much from this.
        #[arg(long, default_value_t = *DEFAULT_MAX_DL_WORKERS)]
        max_download_workers: usize,
        /// How much memory to use to store chunks. Lowering this value will potentially make
        /// downloads slower while being lighter on memory usage. Raising it will make the program
        /// use more memory if needed, but can potentially speed up downloads.
        #[arg(long, default_value_t = *DEFAULT_MAX_MEMORY_USAGE)]
        max_memory_usage: usize,
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
    },
    /// Uninstalls a game
    Uninstall {
        slug: String,
        /// Remove game from installed config but do not delete install folder.
        #[arg(long, default_value_t = false)]
        keep: bool,
    },
    /// Lists available updates for installed games.
    ListUpdates,
    /// Update an installed game.
    Update {
        slug: String,
        /// How many download workers to run at one time.
        /// Increasing this value will make downloads faster, but use more memory.
        /// Lowering this value will lower memory usage at the cost of slower downloads.
        ///
        /// Note: Too many download workers can cause unreliable downloads. The default is
        /// double your CPU_COUNT, or 16, whichever is lowest. You shouldn't deviate too much from
        /// this.
        #[arg(long, default_value_t = *DEFAULT_MAX_DL_WORKERS)]
        max_download_workers: usize,
        /// How much memory to use to store chunks. Lowering this value will potentially make
        /// downloads slower while being lighter on memory usage. Raising it will make the program
        /// use more memory if needed, but can potentially speed up downloads.
        #[arg(long, default_value_t = *DEFAULT_MAX_MEMORY_USAGE)]
        max_memory_usage: usize,
    },
}
