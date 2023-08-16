use clap::{Parser, Subcommand};

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
    Install { slug: String }, // TODO: Install specific version
}
