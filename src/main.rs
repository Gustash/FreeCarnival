use crate::api::auth;
use crate::cli::Cli;
use crate::config::GalaConfig;
use api::{auth::SyncResult, GalaRequest};
use clap::Parser;
use cli::Commands;
use config::{CookieConfig, LibraryConfig, UserConfig};
use prelude::CookieHeaderMap;

mod api;
mod cli;
mod config;
mod constants;
mod prelude;
mod utils;

#[tokio::main]
async fn main() {
    let args = Cli::parse();

    let mut gala_req = GalaRequest::new();

    match args.command {
        Commands::Login { username, password } => {
            let password = match password {
                Some(password) => password,
                None => {
                    rpassword::prompt_password("Password: ").expect("Failed to read from stdin")
                }
            };

            match auth::login(&gala_req.client, &username, &password).await {
                Ok(headers) => {
                    let cookies = headers.to_cookie();
                    gala_req.update_cookies(cookies);
                    match auth::sync(&gala_req.client).await {
                        Ok(result) => save_user_info(&result),
                        Err(err) => println!("Failed to sync: {err:#?}"),
                    }
                }

                Err(err) => println!("Failed to login: {err:#?}"),
            }
        }
        Commands::Logout => {
            UserConfig::clear().expect("Error clearing user config");
            CookieConfig::clear().expect("Error clearing cookies");
            LibraryConfig::clear().expect("Error clearing library");
        }
        Commands::Sync => match auth::sync(&gala_req.client).await {
            Ok(result) => save_user_info(&result),
            Err(err) => println!("Failed to sync: {err:#?}"),
        },
        Commands::Library => {
            let library = LibraryConfig::load().expect("Failed to load library");
            for product in library.collection {
                println!("{}", product);
            }
        }
        Commands::Install {
            slug,
            max_download_workers,
        } => {
            match utils::install(gala_req.client, &slug, max_download_workers).await {
                Ok(true) => {
                    println!("Successfully installed {}", &slug);
                }
                Ok(false) => {
                    println!(
                        "Failed to install {}.\n\nFiles might be missing or corrupted.",
                        &slug
                    );
                }
                Err(err) => {
                    println!("Failed to install {}: {:?}", &slug, err);
                }
            };
        }
    }
}

fn save_user_info(data: &Option<SyncResult>) {
    if let Some(SyncResult {
        user_config,
        cookie_config,
        library_config,
    }) = data
    {
        user_config.store().expect("Failed to save user config");
        cookie_config.store().expect("Failed to save cookies");
        library_config
            .store()
            .expect("Failed to save library config");
    }
}
