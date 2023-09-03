use clap::{Parser, Subcommand};

use crate::constants::DEFAULT_MAX_DL_WORKERS;

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
        #[arg(long, default_value_t = *DEFAULT_MAX_DL_WORKERS)]
        max_download_workers: usize,
    }, // TODO: Install specific version
}
