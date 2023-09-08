use std::{
    collections::{HashMap, HashSet},
    path::PathBuf,
    process::ExitStatus,
    sync::Arc,
    time::Duration,
};

use async_recursion::async_recursion;
use bytes::Bytes;
use directories::ProjectDirs;
use human_bytes::human_bytes;
use os_path::OsPath;
use queues::*;
use regex::Regex;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::{
    fs::File,
    io::AsyncWriteExt,
    sync::{OwnedSemaphorePermit, Semaphore},
    task::JoinHandle,
};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
    },
    config::{GalaConfig, InstalledConfig, LibraryConfig},
    constants::{MAX_CHUNK_SIZE, PROJECT_NAME},
    shared::models::InstallInfo,
};

// TODO: Refactor info printing and chunk downloading to separate functions
pub(crate) async fn install<'a>(
    client: reqwest::Client,
    slug: &String,
    install_path: &PathBuf,
    version: Option<String>,
    max_download_workers: usize,
    max_memory_usage: usize,
    info_only: bool,
    skip_verify: bool,
) -> Result<Result<(String, Option<String>), &'a str>, reqwest::Error> {
    let library = LibraryConfig::load().expect("Failed to load library");
    let product = match library
        .collection
        .iter()
        .find(|p| p.slugged_name == slug.to_owned())
    {
        Some(product) => product,
        None => {
            return Ok(Err("Could not find game in library"));
        }
    };

    println!(
        "Found game. {}",
        match &version {
            Some(version) => format!("Installing build version {}...", version),
            None => String::from("Fetching latest version build number..."),
        }
    );
    let build_version = match version {
        Some(selected) => selected,
        None => match api::product::get_latest_build_number(&client, &product).await? {
            Some(version) => version,
            None => {
                return Ok(Err("Failed to fetch latest build number. Cannot install."));
            }
        },
    };
    println!("Fetching build manifest...");
    let build_manifest =
        api::product::get_build_manifest(&client, &product, &build_version).await?;
    store_build_manifest(
        &build_manifest,
        &build_version,
        &product.slugged_name,
        "manifest",
    )
    .await
    .expect("Failed to save build manifest");

    if info_only {
        let mut build_manifest_rdr = csv::Reader::from_reader(build_manifest.as_bytes());
        let download_size = build_manifest_rdr
            .deserialize::<BuildManifestRecord>()
            .into_iter()
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
        api::product::get_build_manifest_chunks(&client, &product, &build_version).await?;
    store_build_manifest(
        &build_manifest_chunks,
        &build_version,
        &product.slugged_name,
        "manifest_chunks",
    )
    .await
    .expect("Failed to save build manifest chunks");

    let product_arc = Arc::new(product.clone());

    println!("Installing game from manifest...");
    let result = build_from_manifest(
        client,
        product_arc,
        build_manifest.as_bytes(),
        build_manifest_chunks.as_bytes(),
        install_path.into(),
        max_download_workers,
        max_memory_usage,
        skip_verify,
    )
    .await
    .expect("Failed to build from manifest");

    match result {
        true => Ok(Ok((
            format!("Successfully installed {} ({})", slug, build_version),
            Some(build_version),
        ))),
        false => Ok(Err(
            "Some chunks failed verification. Failed to install game.",
        )),
    }
}

pub(crate) async fn uninstall(install_path: &PathBuf) -> tokio::io::Result<()> {
    tokio::fs::remove_dir_all(install_path).await
}

pub(crate) async fn check_updates(
    client: &reqwest::Client,
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
        let latest_version = match api::product::get_latest_build_number(client, product).await {
            Ok(Some(v)) => v,
            Ok(None) => {
                println!("Couldn't find the latest version of {slug}");
                continue;
            }
            Err(err) => {
                println!("Failed to fetch latest version for {slug}: {:?}", err);
                continue;
            }
        };

        if info.version != latest_version {
            available_updates.insert(slug, latest_version);
        }
    }
    Ok(available_updates)
}

// TODO: Allow downgrading
pub(crate) async fn update(
    client: reqwest::Client,
    library: LibraryConfig,
    slug: &String,
    install_info: &InstallInfo,
    selected_version: Option<String>,
    max_download_workers: usize,
    max_memory_usage: usize,
    info_only: bool,
    skip_verify: bool,
) -> tokio::io::Result<(String, Option<String>)> {
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
            match api::product::get_latest_build_number(&client, product).await {
                Ok(Some(v)) => v,
                Ok(None) => {
                    return Ok((format!("Couldn't find the latest version of {slug}"), None));
                }
                Err(err) => {
                    return Ok((
                        format!("Failed to fetch latest version for {slug}: {:?}", err),
                        None,
                    ));
                }
            }
        }
    };

    if install_info.version == version {
        return Ok((format!("Build {version} is already installed"), None));
    }

    let old_manifest = read_build_manifest(&install_info.version, slug, "manifest").await?;

    println!("Fetching {} build manifest...", version);
    let new_manifest = match api::product::get_build_manifest(&client, &product, &version).await {
        Ok(m) => m,
        Err(err) => {
            return Ok((format!("Failed to fetch build manifest: {:?}", err), None));
        }
    };
    store_build_manifest(&new_manifest, &version, slug, "manifest").await?;
    let new_manifest_chunks =
        match api::product::get_build_manifest_chunks(&client, &product, &version).await {
            Ok(m) => m,
            Err(err) => {
                return Ok((
                    format!("Failed to fetch build manifest chunks: {:?}", err),
                    None,
                ));
            }
        };
    store_build_manifest(&new_manifest_chunks, &version, slug, "manifest_chunks").await?;

    let delta_manifest = read_or_generate_delta_manifest(
        slug,
        old_manifest.as_bytes(),
        new_manifest.as_bytes(),
        &install_info.version,
        &version,
    )
    .await?;
    let delta_manifest_chunks = read_or_generate_delta_chunks_manifest(
        slug,
        delta_manifest.as_bytes(),
        new_manifest_chunks.as_bytes(),
        &install_info.version,
        &version,
    )
    .await?;

    if info_only {
        let mut delta_build_manifest_rdr = csv::Reader::from_reader(delta_manifest.as_bytes());
        let download_size = delta_build_manifest_rdr
            .deserialize::<BuildManifestRecord>()
            .into_iter()
            .fold(0f64, |acc, record| match record {
                Ok(record) => match record.tag {
                    Some(ChangeTag::Removed) => acc,
                    _ => acc + record.size_in_bytes as f64,
                },
                Err(_) => acc,
            });
        let mut new_build_manifest_rdr = csv::Reader::from_reader(new_manifest.as_bytes());
        let disk_size = new_build_manifest_rdr
            .deserialize::<BuildManifestRecord>()
            .into_iter()
            .fold(0f64, |acc, record| match record {
                Ok(record) => acc + record.size_in_bytes as f64,
                Err(_) => acc,
            });

        let mut old_manifest_rdr = csv::Reader::from_reader(old_manifest.as_bytes());
        let old_disk_size = old_manifest_rdr
            .deserialize::<BuildManifestRecord>()
            .into_iter()
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
    build_from_manifest(
        client,
        product_arc,
        delta_manifest.as_bytes(),
        delta_manifest_chunks.as_bytes(),
        OsPath::from(&install_info.install_path),
        max_download_workers,
        max_memory_usage,
        skip_verify,
    )
    .await?;

    Ok((format!("Updated {slug} successfully."), Some(version)))
}

pub(crate) async fn launch(
    client: &reqwest::Client,
    product: &Product,
    install_info: &InstallInfo,
    wine_bin: PathBuf,
    wine_prefix: Option<PathBuf>,
) -> tokio::io::Result<Option<ExitStatus>> {
    let game_details = match api::product::get_game_details(&client, &product).await {
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
        Some(path) => OsPath::from(&install_info.install_path).join(path),
        None => match find_exe_recursive(&install_info.install_path).await {
            Some(exe) => exe,
            None => {
                println!("Couldn't find suitable exe...");
                return Ok(None);
            }
        },
    };
    println!("{} was selected", exe);

    let mut command = tokio::process::Command::new(wine_bin);
    command.arg(exe);
    // TODO:
    // Handle cwd and launch args. Since I don't have games that have these I don't have a
    // reliable way to test...
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
    let mut build_manifest_rdr = csv::Reader::from_reader(build_manifest.as_bytes());

    for record in build_manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");

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

#[async_recursion]
async fn find_exe_recursive(path: &PathBuf) -> Option<OsPath> {
    let mut subdirs = vec![];

    match tokio::fs::read_dir(path).await {
        Ok(mut subpath) => {
            while let Ok(Some(entry)) = subpath.next_entry().await {
                let entry_path = entry.path();
                if entry_path.is_file() {
                    // Check if the current path is a file with a .exe extension
                    println!("Checking file: {}", entry_path.display());
                    if let (Some(ext), Some(file_name)) =
                        (entry_path.extension(), entry_path.file_name())
                    {
                        let file_name_str = String::from(match file_name.to_str() {
                            Some(str) => str.to_lowercase(),
                            None => String::new(),
                        });
                        if ext == "exe"
                            && !file_name_str.contains("setup")
                            && !file_name_str.contains("unins")
                        {
                            return Some(OsPath::from(entry_path));
                        }
                    }
                } else if entry_path.is_dir() {
                    subdirs.push(entry_path);
                }
            }
        }
        Err(err) => {
            println!("Failed to iterate over {}: {:?}", path.display(), err);
        }
    }

    for dir in subdirs {
        println!("Checking directory: {}", dir.display());
        if let Some(exe_path) = find_exe_recursive(&dir.to_path_buf()).await {
            return Some(OsPath::from(exe_path));
        }
    }

    None
}

#[derive(PartialEq, Clone, Debug, Serialize, Deserialize)]
pub(crate) enum ChangeTag {
    Added,
    Modified,
    Removed,
}

async fn read_or_generate_delta_manifest(
    slug: &String,
    old_manifest_bytes: &[u8],
    new_manifest_bytes: &[u8],
    old_version: &String,
    new_version: &String,
) -> tokio::io::Result<String> {
    let manifest_delta_version = format!("{}_{}", old_version, new_version);
    if let Ok(exising_delta) =
        read_build_manifest(&manifest_delta_version, slug, "manifest_delta").await
    {
        println!("Using existing delta manifest");
        return Ok(exising_delta);
    }

    println!("Generating delta manifest...");
    let mut new_manifest_rdr = csv::Reader::from_reader(new_manifest_bytes);
    let new_manifest_iter: Vec<BuildManifestRecord> = new_manifest_rdr
        .deserialize::<BuildManifestRecord>()
        .into_iter()
        .map(|r| r.expect("Failed to deserialize updated build manifest"))
        .collect();
    let mut old_manifest_rdr = csv::Reader::from_reader(old_manifest_bytes);
    let old_manifest_iter: Vec<BuildManifestRecord> = old_manifest_rdr
        .deserialize::<BuildManifestRecord>()
        .into_iter()
        .map(|r| r.expect("Failed to deserialize old build manifest"))
        .collect();

    let new_file_names: HashSet<&String> = new_manifest_iter
        .iter()
        .map(|entry| &entry.file_name)
        .collect();
    let mut build_manifest_delta_wtr = csv::Writer::from_writer(vec![]);

    for new_entry in &new_manifest_iter {
        let added = !old_manifest_iter
            .iter()
            .any(|entry| entry.file_name == new_entry.file_name);

        if added {
            println!("{} was added", new_entry.file_name,);
            build_manifest_delta_wtr
                .serialize(BuildManifestRecord {
                    tag: Some(ChangeTag::Added),
                    ..new_entry.clone()
                })
                .expect("Failed to serialize delta build manifest");
            continue;
        }

        let modified = match old_manifest_iter
            .iter()
            .find(|entry| entry.file_name == new_entry.file_name)
        {
            Some(old_entry) => old_entry.sha != new_entry.sha,
            None => false,
        };

        if modified {
            println!("{} was modified", new_entry.file_name,);
            build_manifest_delta_wtr
                .serialize(BuildManifestRecord {
                    tag: Some(ChangeTag::Modified),
                    ..new_entry.clone()
                })
                .expect("Failed to serialize delta build manifest");
        }
    }

    for old_entry in old_manifest_iter {
        if !new_file_names.contains(&old_entry.file_name) {
            println!("{} was deleted", old_entry.file_name);
            build_manifest_delta_wtr
                .serialize(BuildManifestRecord {
                    tag: Some(ChangeTag::Removed),
                    ..old_entry
                })
                .expect("Failed to serialize delta build manifest");
        }
    }
    let delta_str = String::from_utf8(build_manifest_delta_wtr.into_inner().unwrap()).unwrap();
    store_build_manifest(
        &delta_str,
        &format!("{}_{}", old_version, new_version),
        slug,
        "manifest_delta",
    )
    .await?;

    Ok(delta_str)
}

async fn read_or_generate_delta_chunks_manifest(
    slug: &String,
    delta_manifest_bytes: &[u8],
    new_manifest_bytes: &[u8],
    old_version: &String,
    new_version: &String,
) -> tokio::io::Result<String> {
    let manifest_delta_version = format!("{}_{}", old_version, new_version);
    if let Ok(exising_delta) =
        read_build_manifest(&manifest_delta_version, slug, "manifest_delta_chunks").await
    {
        println!("Using existing chunks delta manifest");
        return Ok(exising_delta);
    }

    println!("Generating chunks delta manifest...");
    let mut delta_manifest_rdr = csv::Reader::from_reader(delta_manifest_bytes);
    let mut delta_manifest = delta_manifest_rdr.deserialize::<BuildManifestRecord>();
    let mut current_file = delta_manifest
        .next()
        .expect("Failed to deserialize build manifest delta")
        .expect("There were no changes in this update?");

    let mut new_manifest_rdr = csv::Reader::from_reader(new_manifest_bytes);
    let mut build_manifest_delta_wtr = csv::Writer::from_writer(vec![]);

    for record in new_manifest_rdr.deserialize::<BuildManifestChunksRecord>() {
        let record = record.expect("Failed to deserialize build manifest chunks");
        println!("Current record: {}", record.file_path);

        // Removed files are always last in the delta manifest, so we can break here
        if current_file.tag == Some(ChangeTag::Removed) {
            break;
        }

        // We want to ignore chunks for removed files and folders
        while current_file.is_directory() || current_file.is_empty() {
            current_file = match delta_manifest.next() {
                Some(file) => {
                    println!("Skipping over {}", current_file.file_name);
                    file.expect("Failed to deserialize build manifest delta")
                }
                None => {
                    println!("Done processing delta chunks");
                    break;
                }
            };
        }

        println!("Current file: {}", current_file.file_name);
        if record.file_path != current_file.file_name {
            continue;
        }

        build_manifest_delta_wtr
            .serialize(&record)
            .expect("Failed to serialize build manifest chunks");

        if usize::from(record.id) + 1 == current_file.chunks {
            println!("Done processing chunks for {}", &record.file_path);
            // Move on to the next file
            current_file = match delta_manifest.next() {
                Some(file) => file.expect("Failed to deserialize build manifest delta"),
                None => {
                    println!("Done processing delta chunks");
                    break;
                }
            };
        }
    }

    let delta_str = String::from_utf8(build_manifest_delta_wtr.into_inner().unwrap()).unwrap();
    store_build_manifest(
        &delta_str,
        &format!("{}_{}", old_version, new_version),
        slug,
        "manifest_delta_chunks",
    )
    .await?;

    Ok(delta_str)
}

async fn store_build_manifest(
    body: &String,
    build_number: &String,
    product_slug: &String,
    file_suffix: &str,
) -> tokio::io::Result<()> {
    // TODO: Move appName to constant
    let project = ProjectDirs::from("rs", "", *PROJECT_NAME).unwrap();
    let path = project.config_dir().join("manifests").join(product_slug);
    tokio::fs::create_dir_all(&path).await?;

    let path = path.join(format!("{}_{}.csv", build_number, file_suffix));
    tokio::fs::write(path, body).await
}

async fn read_build_manifest(
    build_number: &String,
    product_slug: &String,
    file_suffix: &str,
) -> tokio::io::Result<String> {
    // TODO: Move appName to constant
    let project = ProjectDirs::from("rs", "", *PROJECT_NAME).unwrap();
    let path = project
        .config_dir()
        .join("manifests")
        .join(product_slug)
        .join(format!("{}_{}.csv", build_number, file_suffix));
    tokio::fs::read_to_string(path).await
}

async fn build_from_manifest(
    client: reqwest::Client,
    product: Arc<Product>,
    build_manifest_bytes: &[u8],
    build_manifest_chunks_bytes: &[u8],
    install_path: OsPath,
    max_download_workers: usize,
    max_memory_usage: usize,
    skip_verify: bool,
) -> tokio::io::Result<bool> {
    let mut write_queue = queue![];
    let mut chunk_queue = queue![];

    // Create install directory if it doesn't exist
    tokio::fs::create_dir_all(&install_path).await?;

    let mut file_chunk_num_map = HashMap::new();

    println!("Building folder structure...");
    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");

        if record.tag == Some(ChangeTag::Modified) || record.tag == Some(ChangeTag::Removed) {
            let file_path = install_path.join(&record.file_name);
            if record.is_directory() {
                // Is a directory
                if file_path.exists() && file_path.is_dir() {
                    // Delete this directory
                    tokio::fs::remove_dir_all(file_path).await?;
                }
                continue;
            }

            if file_path.exists() && file_path.is_file() {
                // Delete this file
                tokio::fs::remove_file(file_path).await?;
            }

            if record.tag == Some(ChangeTag::Removed) {
                continue;
            }
        }

        prepare_file(&install_path, &record.file_name, record.is_directory()).await?;

        if !record.is_directory() {
            file_chunk_num_map.insert(record.file_name.clone(), record.chunks);
        }
    }

    println!("Building queue...");
    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);
    for record in manifest_chunks_rdr.deserialize::<BuildManifestChunksRecord>() {
        let record = record.expect("Failed to deserialize chunks manifest");

        let is_last = file_chunk_num_map[&record.file_path] - 1 == usize::from(record.id);
        if is_last {
            file_chunk_num_map.remove(&record.file_path);
        }
        write_queue
            .add((record.sha.clone(), record.id, is_last))
            .unwrap();
        chunk_queue.add(record).unwrap();
    }
    drop(file_chunk_num_map);

    let (tx, rx) =
        crossbeam_channel::unbounded::<(BuildManifestChunksRecord, Bytes, OwnedSemaphorePermit)>();

    println!("Spawning write thread...");
    let write_handler = tokio::spawn(async move {
        println!("Write thread started.");
        let mut in_buffer = HashMap::new();
        let mut file_map = HashMap::new();
        let max_chunks_in_memory = max_memory_usage / *MAX_CHUNK_SIZE;
        let mut permit_queue = Vec::with_capacity(max_chunks_in_memory);

        while write_queue.size() > 0 {
            let (record, chunk, permit) = match rx.recv_timeout(Duration::from_secs(1)) {
                Ok(msg) => msg,
                Err(_) => {
                    let timeout_ms = 1;
                    println!("Write thread timed out. Waiting {} ms", timeout_ms);
                    // Sleep thread momentarily so other futures can continue
                    tokio::time::sleep(Duration::from_millis(timeout_ms)).await;
                    continue;
                }
            };

            let available_chunks =
                max_chunks_in_memory - std::cmp::min(in_buffer.len(), max_chunks_in_memory);
            if available_chunks >= max_download_workers {
                // We still have space in memory for more chunks, let another download task
                // continue
                drop(permit);
            } else {
                // Memory bank is full of chunks, yummy! Hold on until more chunks are flushed to
                // disk before spawning new download tasks so we don't get a memory stomach ache
                permit_queue.push(permit);
            }
            in_buffer.insert(
                // Some files don't have the chunk id in the sha parts, so they can have reused
                // SHAs for chunks (e.g. DieYoungPrologue-WindowsNoEditor.pak)
                format!("{},{}", record.id, record.sha),
                (record.file_path.clone(), chunk),
            );

            loop {
                match write_queue.peek() {
                    Ok((next_chunk, chunk_id, is_last_chunk)) => {
                        let next_chunk_key = format!("{},{}", chunk_id, next_chunk);
                        if let Some((file_path, bytes)) = in_buffer.remove(&next_chunk_key) {
                            if !file_map.contains_key(&file_path) {
                                let chunk_file_path = install_path.join(&file_path);
                                let file = open_file(&chunk_file_path)
                                    .await
                                    .expect(&format!("Failed to open {}", chunk_file_path));
                                file_map.insert(file_path.clone(), file);
                            }
                            let file = file_map.get_mut(&file_path).unwrap();
                            write_queue.remove().unwrap();
                            println!("Writing {}", next_chunk);
                            append_chunk(file, bytes).await.expect(&format!(
                                "Failed to write {}.bin to {}",
                                next_chunk, file_path
                            ));

                            if is_last_chunk {
                                file_map.remove(&file_path);
                            }

                            // Let another download task go since we have flushed this chunk to
                            // disk
                            permit_queue.pop();

                            continue;
                        }

                        println!(
                            "Not ready to write {}: {} pending",
                            next_chunk,
                            in_buffer.len()
                        );

                        break;
                    }
                    Err(_) => {
                        println!("No more chunks to write");
                        return;
                    }
                }
            }
        }
        println!("Write thread finished.");
    });

    println!("Downloading chunks...");
    let semaphore = Arc::new(Semaphore::new(max_download_workers));
    while let Ok(record) = chunk_queue.remove() {
        let client = client.clone();
        let product = product.clone();
        let thread_tx = tx.clone();
        let permit = semaphore.clone().acquire_owned().await.unwrap();

        tokio::spawn(async move {
            println!("Downloading {}", record.sha);
            let chunk = api::product::download_chunk(&client, &product, &record.sha)
                .await
                .expect(&format!("Failed to download {}.bin", &record.sha));

            let chunk_parts = &record.sha.split("_").collect::<Vec<&str>>();
            match chunk_parts.last() {
                Some(chunk_sha) => {
                    println!("Verifying {}", record.sha);
                    let chunk_corrupted = !verify_chunk(&chunk, chunk_sha);

                    if chunk_corrupted {
                        println!(
                            "{} failed verification. {} is corrupted.",
                            &record.sha, &record.file_path
                        );
                        return false;
            if !skip_verify {
                let chunk_parts = &record.sha.split("_").collect::<Vec<&str>>();
                match chunk_parts.last() {
                    Some(chunk_sha) => {
                        // println!("Verifying {}", record.sha);
                        let chunk_corrupted = !verify_chunk(&chunk, chunk_sha);

                        if chunk_corrupted {
                            println!(
                                "{} failed verification. {} is corrupted.",
                                &record.sha, &record.file_path
                            );
                            return false;
                        }
                    }
                    None => {
                        println!("Couldn't find Chunk SHA. Skipping verification...");
                    }
                }
            }

            thread_tx.send((record, chunk, permit)).unwrap();

            true
        });
    }

    println!("Waiting for write thread to finish...");
    write_handler.await?;

    // TODO: Redo logic for verification
    Ok(true)
}

async fn open_file(file_path: &OsPath) -> tokio::io::Result<File> {
    tokio::fs::OpenOptions::new()
        .append(true)
        .open(file_path)
        .await
}

async fn append_chunk(file: &mut tokio::fs::File, chunk: Bytes) -> tokio::io::Result<()> {
    file.write_all(&chunk).await
}

async fn prepare_file(
    base_install_path: &OsPath,
    file_name: &String,
    is_directory: bool,
) -> tokio::io::Result<()> {
    let file_path = base_install_path.join(file_name);

    // File is a directory. We should create this directory.
    if is_directory {
        if !file_path.exists() {
            tokio::fs::create_dir(&file_path).await?;
        }
        return Ok(());
    }

    // Create empty file.
    tokio::fs::File::create(&file_path).await?;

    Ok(())
}

fn verify_file_hash(file_path: &OsPath, sha: &str) -> std::io::Result<bool> {
    let mut file = std::fs::File::open(file_path)?;
    let mut hasher = Sha256::new();
    std::io::copy(&mut file, &mut hasher)?;
    let hash = hasher.finalize();
    let file_sha = base16ct::lower::encode_string(&hash);

    Ok(file_sha == sha)
}

fn verify_chunk(chunk: &Bytes, sha: &str) -> bool {
    let mut hasher = Sha256::new();
    hasher.update(chunk);
    let hash = hasher.finalize();
    let sha_str = base16ct::lower::encode_string(&hash);

    sha_str == sha
}
