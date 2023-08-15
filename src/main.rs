use crate::api::auth;
use crate::cli::Cli;
use clap::Parser;
use cli::Commands;
use prelude::GalaConfig;

mod api;
mod cli;
mod config;
mod constants;
mod prelude;

#[tokio::main]
async fn main() {
    let args = Cli::parse();

    match args.command {
        Commands::Login { username, password } => {
            let password = match password {
                Some(password) => password,
                None => {
                    rpassword::prompt_password("Password: ").expect("Failed to read from stdin")
                }
            };

            match auth::login(&username, &password).await {
                Ok(user_config) => {
                    if let Some(user_config) = user_config {
                        user_config.store().expect("Failed to save user config");
                    }
                }
                Err(err) => println!("Failed to login: {err:#?}"),
            }
        }
    }
}
