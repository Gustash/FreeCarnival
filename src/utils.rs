use std::{collections::HashMap, path::PathBuf, process::ExitStatus, sync::Arc};

use human_bytes::human_bytes;
use os_path::OsPath;
use regex::Regex;
use shlex::split;
use tokio::task::JoinHandle;

#[cfg(target_os = "macos")]
use crate::helpers::mac::{find_app_recursive, find_info_plist, MacAppExecutables};
use crate::{
    api,
    cli::InstallOpts,
    config::{GalaConfig, InstalledConfig, LibraryConfig},
    helpers::{
        build_from_manifest, find_exe_recursive, read_build_manifest,
        read_or_generate_delta_chunks_manifest, read_or_generate_delta_manifest,
        store_build_manifest, verify_file_hash,
    },
    shared::models::{
        api::{BuildOs, Product, ProductVersion},
        BuildManifestRecord, ChangeTag, InstallInfo,
    },
};

// TODO: Refactor info printing and chunk downloading to separate functions
pub(crate) async fn install<'a>(
    client: reqwest::Client,
    slug: &String,
    install_path: &PathBuf,
    install_opts: InstallOpts,
    version: Option<&ProductVersion>,
    os: Option<BuildOs>,
) -> Result<Result<(String, Option<InstallInfo>), &'a str>, reqwest::Error> {
    let library = LibraryConfig::load().expect("Failed to load library");
    let product = match library.collection.iter().find(|p| p.slugged_name == *slug) {
        Some(product) => product,
        None => {
            return Ok(Err("Could not find game in library"));
        }
    };

    let build_version = match version {
        Some(selected) => selected,
        None => match product.get_latest_version(os.as_ref()) {
            Some(latest) => latest,
            None => {
                return Ok(Err("Failed to fetch latest build number. Cannot install."));
            }
        },
    };
    println!("Found game. Installing build version {}...", build_version);

    println!("Fetching build manifest...");
    let build_manifest = api::product::get_build_manifest(&client, product, build_version).await?;
    store_build_manifest(
        &build_manifest,
        &build_version.version,
        &product.slugged_name,
        "manifest",
    )
    .await
    .expect("Failed to save build manifest");

    if install_opts.info {
        let mut build_manifest_rdr = csv::Reader::from_reader(&build_manifest[..]);
        let download_size = build_manifest_rdr
            .byte_records()
            .map(|r| {
                let mut record = r.expect("Failed to get byte record");
                record.push_field(b"");
                record.deserialize::<BuildManifestRecord>(None)
            })
            .fold(0f64, |acc, record| match record {
                Ok(record) => acc + record.size_in_bytes as f64,
                Err(_) => acc,
            });

        let mut buf = String::new();
        buf.push_str(&format!("Download Size: {}", human_bytes(download_size)));
        buf.push_str(&format!("\nDisk Size: {}", human_bytes(download_size)));
        return Ok(Ok((buf, None)));
    }

    println!("Fetching build manifest chunks...");
    let build_manifest_chunks =
        api::product::get_build_manifest_chunks(&client, product, build_version).await?;
    store_build_manifest(
        &build_manifest_chunks,
        &build_version.version,
        &product.slugged_name,
        "manifest_chunks",
    )
    .await
    .expect("Failed to save build manifest chunks");

    let product_arc = Arc::new(product.clone());
    let os_arc = Arc::new(build_version.os.to_owned());

    println!("Installing game from manifest...");
    let result = build_from_manifest(
        client,
        product_arc,
        os_arc,
        &build_manifest[..],
        &build_manifest_chunks[..],
        install_path.into(),
        install_opts,
    )
    .await
    .expect("Failed to build from manifest");

    match result {
        true => {
            let install_info = InstallInfo::new(
                install_path.to_owned(),
                build_version.version.to_owned(),
                build_version.os.to_owned(),
            );
            Ok(Ok((
                format!("Successfully installed {} ({})", slug, build_version),
                Some(install_info),
            )))
        }
        false => Ok(Err(
            "Some chunks failed verification. Failed to install game.",
        )),
    }
}

pub(crate) async fn uninstall(install_path: &PathBuf) -> tokio::io::Result<()> {
    tokio::fs::remove_dir_all(install_path).await
}

pub(crate) async fn check_updates(
    library: LibraryConfig,
    installed: InstalledConfig,
) -> tokio::io::Result<HashMap<String, String>> {
    let mut available_updates = HashMap::new();
    for (slug, info) in installed {
        println!("Checking if {slug} has updates...");
        let product = match library.collection.iter().find(|p| p.slugged_name == slug) {
            Some(p) => p,
            None => {
                println!("Couldn't find {slug} in library. Try running `sync` first.");
                continue;
            }
        };
        let latest_version = match product.get_latest_version(Some(&info.os)) {
            Some(v) => v,
            None => {
                println!("Couldn't find the latest version of {slug}");
                continue;
            }
        };

        if info.version != latest_version.version {
            available_updates.insert(slug, latest_version.version.to_owned());
        }
    }
    Ok(available_updates)
}

pub(crate) async fn update(
    client: reqwest::Client,
    library: &LibraryConfig,
    slug: &String,
    install_opts: InstallOpts,
    install_info: &InstallInfo,
    selected_version: Option<&ProductVersion>,
) -> tokio::io::Result<(String, Option<InstallInfo>)> {
    let product = match library.collection.iter().find(|p| &p.slugged_name == slug) {
        Some(p) => p,
        None => {
            return Ok((format!("Couldn't find {slug} in library"), None));
        }
    };
    let version = match selected_version {
        Some(v) => v,
        None => {
            println!("Fetching latest version...");
            match product.get_latest_version(Some(&install_info.os)) {
                Some(v) => v,
                None => {
                    return Ok((format!("Couldn't find the latest version of {slug}"), None));
                }
            }
        }
    };

    if install_info.version == version.version {
        return Ok((format!("Build {version} is already installed"), None));
    }

    let old_manifest = read_build_manifest(&install_info.version, slug, "manifest").await?;

    println!("Fetching {} build manifest...", version);
    let new_manifest = match api::product::get_build_manifest(&client, product, version).await {
        Ok(m) => m,
        Err(err) => {
            return Ok((format!("Failed to fetch build manifest: {:?}", err), None));
        }
    };
    store_build_manifest(&new_manifest, &version.version, slug, "manifest").await?;
    let new_manifest_chunks =
        match api::product::get_build_manifest_chunks(&client, product, version).await {
            Ok(m) => m,
            Err(err) => {
                return Ok((
                    format!("Failed to fetch build manifest chunks: {:?}", err),
                    None,
                ));
            }
        };
    store_build_manifest(
        &new_manifest_chunks,
        &version.version,
        slug,
        "manifest_chunks",
    )
    .await?;

    let delta_manifest = read_or_generate_delta_manifest(
        slug,
        &old_manifest[..],
        &new_manifest[..],
        &install_info.version,
        &version.version,
    )
    .await?;
    let delta_manifest_chunks = read_or_generate_delta_chunks_manifest(
        slug,
        &delta_manifest[..],
        &new_manifest_chunks[..],
        &install_info.version,
        &version.version,
    )
    .await?;

    if install_opts.info {
        let mut delta_build_manifest_rdr = csv::Reader::from_reader(&delta_manifest[..]);
        let download_size = delta_build_manifest_rdr
            .byte_records()
            .map(|r| {
                r.expect("Failed to get byte record")
                    .deserialize::<BuildManifestRecord>(None)
            })
            .fold(0f64, |acc, record| match record {
                Ok(record) => match record.tag {
                    Some(ChangeTag::Removed) => acc,
                    _ => acc + record.size_in_bytes as f64,
                },
                Err(_) => acc,
            });
        let mut new_build_manifest_rdr = csv::Reader::from_reader(&new_manifest[..]);
        let disk_size = new_build_manifest_rdr
            .byte_records()
            .map(|r| {
                let mut record = r.expect("Failed to get byte record");
                record.push_field(b"");
                record.deserialize::<BuildManifestRecord>(None)
            })
            .fold(0f64, |acc, record| match record {
                Ok(record) => acc + record.size_in_bytes as f64,
                Err(_) => acc,
            });

        let mut old_manifest_rdr = csv::Reader::from_reader(&old_manifest[..]);
        let old_disk_size = old_manifest_rdr
            .byte_records()
            .map(|r| {
                let mut record = r.expect("Failed to get byte record");
                record.push_field(b"");
                record.deserialize::<BuildManifestRecord>(None)
            })
            .fold(0f64, |acc, record| match record {
                Ok(record) => acc + record.size_in_bytes as f64,
                Err(_) => acc,
            });

        let needed_space = disk_size - old_disk_size;
        println!("{}", needed_space);

        let mut buf = String::new();
        buf.push_str(&format!("Download Size: {}", human_bytes(download_size)));
        buf.push_str(&format!(
            "\nNeeded Space: {}{}",
            if needed_space < 0f64 { "-" } else { "" },
            human_bytes(needed_space.abs())
        ));
        buf.push_str(&format!("\nTotal Disk Size: {}", human_bytes(disk_size)));
        return Ok((buf, None));
    }

    let product_arc = Arc::new(product.clone());
    let version_arc = Arc::new(version.os.to_owned());
    build_from_manifest(
        client,
        product_arc,
        version_arc,
        &delta_manifest[..],
        &delta_manifest_chunks[..],
        OsPath::from(&install_info.install_path),
        install_opts,
    )
    .await?;

    let install_info = InstallInfo::new(
        install_info.install_path.to_owned(),
        version.version.to_owned(),
        version.os.to_owned(),
    );
    Ok((format!("Updated {slug} successfully."), Some(install_info)))
}

pub(crate) async fn launch(
    client: &reqwest::Client,
    product: &Product,
    install_info: &InstallInfo,
    #[cfg(not(target_os = "windows"))] no_wine: bool,
    #[cfg(not(target_os = "windows"))] wine_bin: Option<PathBuf>,
    #[cfg(not(target_os = "windows"))] wine_prefix: Option<PathBuf>,
    wrapper: Option<PathBuf>,
) -> tokio::io::Result<Option<ExitStatus>> {
    let os = &install_info.os;

    #[cfg(not(target_os = "windows"))]
    let wine_bin = match os {
        BuildOs::Windows => match wine_bin {
            Some(wine_bin) => Some(wine_bin),
            None => {
                if !no_wine {
                    println!("You need to set --wine-bin to run Windows games");
                    return Ok(None);
                } else {
                    None
                }
            }
        },
        _ => None,
    };

    let game_details = match api::product::get_game_details(client, product).await {
        Ok(details) => details,
        Err(err) => {
            println!("Failed to fetch game details. Launch might fail: {:?}", err);

            None
        }
    };

    let exe_path = match game_details {
        Some(details) => match details.exe_path {
            Some(exe_path) => {
                // Not too sure about this. At least syberia-ii prepends the slugged name to the
                // path of the exe. I assume the galaClient always installs in folders with the
                // slugged name, but since we don't do that here, we skip it.
                // This might break if some games don't do this, and if that happens, we should
                // find a better solution for handling this.
                let re = Regex::new(&format!("^{}\\\\", product.slugged_name)).unwrap();
                let dirless_path = re.replace(&exe_path, "");

                Some(dirless_path.into_owned())
            }
            None => None,
        },
        None => None,
    };
    let exe = match exe_path {
        Some(path) => OsPath::from(&install_info.install_path)
            .join(path)
            .to_pathbuf(),
        None => match os {
            BuildOs::Windows => match find_exe_recursive(&install_info.install_path).await {
                Some(exe) => exe,
                None => {
                    println!("Couldn't find suitable exe...");
                    return Ok(None);
                }
            },
            #[cfg(target_os = "macos")]
            BuildOs::Mac => match find_app_recursive(&install_info.install_path).await {
                Some(app) => {
                    let plist = find_info_plist(&app);
                    let mac_executables = MacAppExecutables::with_plist(plist);

                    match mac_executables.executable() {
                        Some(exe) => exe,
                        None => {
                            println!("Couldn't find executable in Info.plist...");
                            return Ok(None);
                        }
                    }
                }
                None => {
                    println!("Couldn't find a suitable app...");
                    return Ok(None);
                }
            },
            #[cfg(not(target_os = "macos"))]
            BuildOs::Mac => {
                println!("You can only launch macOS games on macOS");
                return Ok(None);
            }
            BuildOs::Linux => {
                println!("We don't support launching Linux games yet...");
                return Ok(None);
            }
        },
    };
    println!("{} was selected", exe.display());

    #[cfg(not(target_os = "windows"))]
    let should_use_wine = (os == &BuildOs::Windows) && !no_wine;
    #[cfg(target_os = "windows")]
    let should_use_wine = false;
    #[cfg(target_os = "windows")]
    let wine_bin: Option<PathBuf> = None;
    let wrapper_string = if wrapper.is_some() {
            wrapper.unwrap_or_default().to_str().unwrap().to_owned()
        } else {
            "".to_owned()
        };
    let wrapper_vec = if !wrapper_string.is_empty() {
        split(&wrapper_string.to_owned()).unwrap()
    } else {
        Vec::<String>::new()
    };
    let binary = 
        if wrapper_vec.len() > 0 {
            wrapper_vec[0].to_owned()
        } else {
            if should_use_wine {
                wine_bin.unwrap().to_str().unwrap().to_owned()
            } else {
                exe.to_str().unwrap().to_owned()
            }
        };

    let mut command = tokio::process::Command::new(binary);
    if wrapper_vec.len() > 1 {
        for val in wrapper_vec.iter().skip(1) {
            command.arg(val);
        };
    };

    if !wrapper_string.is_empty() || should_use_wine {
        command.arg(exe.to_str().unwrap().to_owned());
    };
    // TODO:
    // Handle cwd and launch args. Since I don't have games that have these I don't have a
    // reliable way to test...
    #[cfg(not(target_os = "windows"))]
    if let Some(wine_prefix) = wine_prefix {
        command.env("WINEPREFIX", wine_prefix);
    }
    let mut child = command.spawn()?;

    let status = child.wait().await?;

    Ok(Some(status))
}

pub(crate) async fn verify(slug: &String, install_info: &InstallInfo) -> tokio::io::Result<bool> {
    let mut handles: Vec<JoinHandle<bool>> = vec![];

    let build_manifest = read_build_manifest(&install_info.version, slug, "manifest").await?;
    let mut build_manifest_rdr = csv::Reader::from_reader(&build_manifest[..]);
    let build_manifest_byte_records = build_manifest_rdr.byte_records();

    for record in build_manifest_byte_records {
        let mut record = record.expect("Failed to get byte record");
        record.push_field(b"");
        let record = record
            .deserialize::<BuildManifestRecord>(None)
            .expect("Failed to deserialize build manifest");

        if record.is_directory() {
            continue;
        }

        let file_path = OsPath::from(install_info.install_path.join(&record.file_name));
        if !tokio::fs::try_exists(&file_path).await? {
            println!("{} is missing", record.file_name);
            return Ok(false);
        }

        handles.push(tokio::spawn(async move {
            match verify_file_hash(&file_path, &record.sha) {
                Ok(result) => result,
                Err(err) => {
                    println!("Failed to verify {}: {:?}", record.file_name, err);

                    false
                }
            }
        }));
    }

    let mut result = true;
    for handle in handles {
        if !handle.await? {
            result = false;
            break;
        }
    }

    Ok(result)
}
