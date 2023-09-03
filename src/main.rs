use crate::cli::Cli;
use crate::config::GalaConfig;
use crate::shared::models::InstallInfo;
use crate::{api::auth, config::InstalledConfig};
use api::{auth::SyncResult, GalaRequest};
use clap::Parser;
use cli::Commands;
use config::{CookieConfig, LibraryConfig, UserConfig};
use constants::DEFAULT_BASE_INSTALL_PATH;
use prelude::CookieHeaderMap;

mod api;
mod cli;
mod config;
mod constants;
mod prelude;
mod shared;
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
            version,
            max_download_workers,
            path,
            base_path,
        } => {
            let mut installed = InstalledConfig::load().expect("Failed to load installed");
            if installed.contains_key(&slug) {
                println!("{slug} already installed.");
                return;
            }

            let install_path = match (path, base_path) {
                (Some(path), _) => path,
                (None, Some(base_path)) => base_path.join(&slug),
                (None, None) => DEFAULT_BASE_INSTALL_PATH.join(&slug),
            };
            match utils::install(
                gala_req.client,
                &slug,
                &install_path,
                version,
                max_download_workers,
            )
            .await
            {
                Ok(Ok(installed_version)) => {
                    println!("Successfully installed {} ({})", &slug, &installed_version);

                    installed.insert(slug, InstallInfo::new(install_path, installed_version));
                    installed
                        .store()
                        .expect("Failed to update installed config");
                }
                Ok(Err(err)) => {
                    println!("Failed to install {}: {:?}", &slug, err);
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
