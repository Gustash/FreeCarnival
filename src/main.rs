use crate::api::auth;
use crate::cli::Cli;
use clap::Parser;
use cli::Commands;

mod api;
mod cli;
mod constants;

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
                Ok(user_info) => println!("User Info: {:#?}", user_info),
                Err(err) => println!("Failed to login: {err:#?}"),
            }
        }
    }
}
