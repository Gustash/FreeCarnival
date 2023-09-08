use std::sync::Arc;

use crate::cli::Cli;
use crate::config::GalaConfig;
use crate::shared::models::InstallInfo;
use crate::{api::auth, config::InstalledConfig};
use api::auth::{LoginResult, SyncResult};
use api::GalaClient;
use clap::Parser;
use cli::Commands;
use config::{CookieConfig, LibraryConfig, UserConfig};
use constants::DEFAULT_BASE_INSTALL_PATH;
use reqwest_cookie_store::CookieStoreMutex;

mod api;
mod cli;
mod config;
mod constants;
mod shared;
mod utils;

#[tokio::main]
async fn main() {
    let args = Cli::parse();
    let CookieConfig(cookie_store) = CookieConfig::load().expect("Failed to load cookie store");
    let cookie_store = Arc::new(CookieStoreMutex::new(cookie_store));
    let client = reqwest::Client::with_gala(&cookie_store);

    match args.command {
        Commands::Login { username, password } => {
            let password = match password {
                Some(password) => password,
                None => {
                    rpassword::prompt_password("Password: ").expect("Failed to read from stdin")
                }
            };

            match auth::login(&client, &username, &password).await {
                Ok(Some(LoginResult { message, status })) => {
                    if status != "success" {
                        println!("Login failed: {}", message);
                        return;
                    }

                    match auth::sync(&client).await {
                        Ok(result) => save_user_info(&result),
                        Err(err) => println!("Failed to sync: {err:#?}"),
                    };
                }
                Ok(None) => {
                    println!("Failed to parse login response");
                }
                Err(err) => println!("Failed to login: {err:#?}"),
            }
        }
        Commands::Logout => {
            UserConfig::clear().expect("Error clearing user config");
            CookieConfig::clear().expect("Error clearing cookies");
            LibraryConfig::clear().expect("Error clearing library");
        }
        Commands::Sync => match auth::sync(&client).await {
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
            max_memory_usage,
            path,
            base_path,
            info,
            skip_verify,
        } => {
            let mut installed = InstalledConfig::load().expect("Failed to load installed");
            if installed.contains_key(&slug) && !info {
                println!("{slug} already installed.");
                return;
            }

            let install_path = match (path, base_path) {
                (Some(path), _) => path,
                (None, Some(base_path)) => base_path.join(&slug),
                (None, None) => DEFAULT_BASE_INSTALL_PATH.join(&slug),
            };
            match utils::install(
                client.clone(),
                &slug,
                &install_path,
                version,
                max_download_workers,
                max_memory_usage,
                info,
                skip_verify,
            )
            .await
            {
                Ok(Ok((info, Some(installed_version)))) => {
                    println!("{}", info);

                    installed.insert(slug, InstallInfo::new(install_path, installed_version));
                    installed
                        .store()
                        .expect("Failed to update installed config");
                }
                Ok(Ok((info, None))) => {
                    println!("{}", info);
                }
                Ok(Err(err)) => {
                    println!("Failed to install {}: {:?}", &slug, err);
                }
                Err(err) => {
                    println!("Failed to install {}: {:?}", &slug, err);
                }
            };
        }
        Commands::Uninstall { slug, keep } => {
            let mut installed = InstalledConfig::load().expect("Failed to load installed");
            let install_info = match installed.remove(&slug) {
                Some(info) => info,
                None => {
                    println!("{slug} is not installed.");
                    return;
                }
            };

            let folder_removed = if keep {
                false
            } else {
                match utils::uninstall(&install_info.install_path).await {
                    Ok(()) => true,
                    Err(err) => {
                        println!("Failed to uninstall {slug}: {:?}", err);
                        false
                    }
                }
            };
            installed
                .store()
                .expect("Failed to update installed config");
            println!(
                "{slug} uninstalled successfuly. {} was {}.",
                install_info.install_path.display(),
                if folder_removed {
                    "removed"
                } else {
                    "not removed"
                }
            );
        }
        Commands::ListUpdates => {
            let installed = InstalledConfig::load().expect("Failed to load installed");
            let library = LibraryConfig::load().expect("Failed to load library");

            match utils::check_updates(&client, library, installed).await {
                Ok(available_updates) => {
                    if available_updates.is_empty() {
                        println!("No available updates");
                        return;
                    }

                    for (slug, latest_version) in available_updates {
                        println!("{slug} has an update -> {latest_version}");
                    }
                }
                Err(err) => {
                    println!("Failed to check for updates: {:?}", err);
                }
            };
        }
        Commands::Update {
            slug,
            version,
            max_download_workers,
            max_memory_usage,
            info,
            skip_verify,
        } => {
            let mut installed = InstalledConfig::load().expect("Failed to load installed");
            let install_info = match installed.remove(&slug) {
                Some(info) => info,
                None => {
                    println!("{slug} is not installed.");
                    return;
                }
            };
            let library = LibraryConfig::load().expect("Failed to load library");

            match utils::update(
                client.clone(),
                library,
                &slug,
                &install_info,
                version,
                max_download_workers,
                max_memory_usage,
                info,
                skip_verify,
            )
            .await
            {
                Ok((info, Some(installed_version))) => {
                    println!("{}", info);
                    installed.insert(
                        slug,
                        InstallInfo::new(install_info.install_path, installed_version),
                    );
                    installed
                        .store()
                        .expect("Failed to update installed config");
                }
                Ok((info, None)) => {
                    println!("{}", info);
                }
                Err(err) => {
                    println!("Failed to update {slug}: {:?}", err);
                }
            };
        }
        Commands::Launch {
            slug,
            wine_bin,
            wine_prefix,
        } => {
            let installed = InstalledConfig::load().expect("Failed to load installed");
            let library = LibraryConfig::load().expect("Failed to load library");
            let install_info = match installed.get(&slug) {
                Some(info) => info,
                None => {
                    println!("{slug} is not installed");
                    return;
                }
            };
            let product = match library.collection.iter().find(|p| p.slugged_name == slug) {
                Some(prod) => prod,
                None => {
                    println!("Couldn't find {slug} in library");
                    return;
                }
            };

            match utils::launch(&client, product, install_info, wine_bin, wine_prefix).await {
                Ok(Some(status)) => {
                    println!("Process exited with: {}", status);
                }
                Ok(None) => {
                    println!("Failed to launch {slug}");
                }
                Err(err) => {
                    println!("Failed to launch {}: {:?}", slug, err);
                }
            };
        }
        Commands::Info { slug } => {
            let library = LibraryConfig::load().expect("Failed to load library");
            let product = match library.collection.iter().find(|p| p.slugged_name == slug) {
                Some(p) => p,
                None => {
                    println!("{slug} is not in your library");
                    return;
                }
            };

            let installed = InstalledConfig::load().expect("Failed to load installed");
            let install_info = installed.get(&slug);

            println!(
                "Available Versions:\n{}",
                product
                    .version
                    .iter()
                    .map(|v| format!("\n{}", v))
                    .collect::<Vec<String>>()
                    .join("\n")
            );
        }
        Commands::Verify { slug } => {
            let installed = InstalledConfig::load().expect("Failed to load installed");
            let install_info = match installed.get(&slug) {
                Some(info) => info,
                None => {
                    println!("{slug} is not installed.");
                    return;
                }
            };

            match utils::verify(&slug, install_info).await {
                Ok(true) => {
                    println!("{slug} passed verification.");
                }
                Ok(false) => {
                    println!("{slug} is corrupted. Please reinstall.");
                }
                Err(err) => {
                    println!("Failed to verify files: {}", err);
                }
            }
        }
    };

    drop(client);
    let cookie_store = Arc::try_unwrap(cookie_store).expect("Failed to unwrap cookie store");
    let cookie_store = cookie_store
        .into_inner()
        .expect("Failed to unwrap CookieStoreMutex");
    CookieConfig(cookie_store)
        .store()
        .expect("Failed to save cookie config");
}

fn save_user_info(data: &Option<SyncResult>) {
    if let Some(SyncResult {
        user_config,
        library_config,
    }) = data
    {
        user_config.store().expect("Failed to save user config");
        library_config
            .store()
            .expect("Failed to save library config");
    }
}
