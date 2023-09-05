use std::{
    collections::{HashMap, HashSet},
    path::PathBuf,
    sync::Arc,
    time::Duration,
};

use async_recursion::async_recursion;
use bytes::Bytes;
use directories::ProjectDirs;
use os_path::OsPath;
use queues::*;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::{
    fs::File,
    io::AsyncWriteExt,
    sync::{OwnedSemaphorePermit, Semaphore},
};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
    },
    config::{GalaConfig, InstalledConfig, LibraryConfig},
    constants::MAX_CHUNK_SIZE,
    shared::models::InstallInfo,
};

pub(crate) async fn install<'a>(
    client: reqwest::Client,
    slug: &String,
    install_path: &PathBuf,
    version: Option<String>,
    max_download_workers: usize,
    max_memory_usage: usize,
) -> Result<Result<String, &'a str>, reqwest::Error> {
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
    )
    .await
    .expect("Failed to build from manifest");

    match result {
        true => Ok(Ok(build_version)),
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
) -> tokio::io::Result<Option<String>> {
    let product = match library.collection.iter().find(|p| &p.slugged_name == slug) {
        Some(p) => p,
        None => {
            println!("Couldn't find {slug} in library");
            return Ok(None);
        }
    };
    let version = match selected_version {
        Some(v) => v,
        None => match api::product::get_latest_build_number(&client, product).await {
            Ok(Some(v)) => v,
            Ok(None) => {
                println!("Couldn't find the latest version of {slug}");
                return Ok(None);
            }
            Err(err) => {
                println!("Failed to fetch latest version for {slug}: {:?}", err);
                return Ok(None);
            }
        },
    };

    if install_info.version == version {
        println!("Build {version} is already installed");
        return Ok(None);
    }

    let old_manifest = read_build_manifest(&install_info.version, slug, "manifest").await?;

    let new_manifest = match api::product::get_build_manifest(&client, &product, &version).await {
        Ok(m) => m,
        Err(err) => {
            println!("Failed to fetch build manifest: {:?}", err);
            return Ok(None);
        }
    };
    store_build_manifest(&new_manifest, &version, slug, "manifest").await?;
    let new_manifest_chunks =
        match api::product::get_build_manifest_chunks(&client, &product, &version).await {
            Ok(m) => m,
            Err(err) => {
                println!("Failed to fetch build manifest chunks: {:?}", err);
                return Ok(None);
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

    let product_arc = Arc::new(product.clone());
    build_from_manifest(
        client,
        product_arc,
        delta_manifest.as_bytes(),
        delta_manifest_chunks.as_bytes(),
        OsPath::from(&install_info.install_path),
        max_download_workers,
        max_memory_usage,
    )
    .await?;

    Ok(Some(version))
}

pub(crate) async fn launch(install_info: &InstallInfo) -> tokio::io::Result<()> {
    match find_exe_recursive(&install_info.install_path).await {
        Some(exe) => {
            println!("{} was selected", exe.display());
        }
        None => {
            println!("Couldn't find suitable exe...");
        }
    };

    Ok(())
}

#[async_recursion]
async fn find_exe_recursive(path: &PathBuf) -> Option<PathBuf> {
    let mut subdirs = vec![];

    match tokio::fs::read_dir(path).await {
        Ok(mut subpath) => {
            while let Ok(Some(entry)) = subpath.next_entry().await {
                let entry_path = entry.path();
                if entry_path.is_file() {
                    // Check if the current path is a file with a .exe extension
                    println!("Checking file: {}", entry_path.display());
                    if let Some(ext) = entry_path.extension() {
                        if ext == "exe" {
                            return Some(entry_path.to_path_buf());
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
            return Some(exe_path);
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
    let old_manifest_hash: Vec<BuildManifestRecord> = old_manifest_rdr
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
        if !old_manifest_hash
            .iter()
            .any(|entry| entry.file_name == new_entry.file_name)
        {
            println!("{} was added", new_entry.file_name);
            build_manifest_delta_wtr
                .serialize(BuildManifestRecord {
                    tag: Some(ChangeTag::Added),
                    ..new_entry.clone()
                })
                .expect("Failed to serialize delta build manifest");
        }
    }

    for old_entry in old_manifest_hash {
        if let Some(new_entry) = new_manifest_iter
            .iter()
            .find(|entry| entry.file_name == old_entry.file_name)
        {
            if old_entry.sha != new_entry.sha {
                println!("{} was modified", old_entry.file_name);
                build_manifest_delta_wtr
                    .serialize(BuildManifestRecord {
                        tag: Some(ChangeTag::Modified),
                        ..new_entry.clone()
                    })
                    .expect("Failed to serialize delta build manifest");
            }
            continue;
        }

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

        // We want to ignore chunks for removed files
        while current_file.tag == Some(ChangeTag::Removed) {
            current_file = match delta_manifest.next() {
                Some(file) => file.expect("Failed to deserialize build manifest delta"),
                None => {
                    println!("Done processing delta chunks");
                    break;
                }
            };
        }

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
    let project = ProjectDirs::from("rs", "", "openGala").unwrap();
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
    let project = ProjectDirs::from("rs", "", "openGala").unwrap();
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
            if record.flags == 40 {
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

        prepare_file_folder(&install_path, &record.file_name, record.flags).await?;

        if record.flags != 40 {
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
                    }
                }
                None => {
                    println!("Couldn't find Chunk SHA. Skipping verification...");
                }
            }

            println!(
                "Sending {} to writer thread ({})",
                record.sha,
                if thread_tx.is_empty() {
                    "empty"
                } else {
                    "not empty"
                }
            );
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
        .create(true)
        .append(true)
        .open(file_path)
        .await
}

async fn append_chunk(file: &mut tokio::fs::File, chunk: Bytes) -> tokio::io::Result<()> {
    file.write_all(&chunk).await
}

async fn prepare_file_folder(
    base_install_path: &OsPath,
    file_name: &String,
    flags: u8,
) -> tokio::io::Result<()> {
    let file_path = base_install_path.join(file_name);

    // File Name is a directory. We should create this directory.
    if flags == 40 && !file_path.exists() {
        tokio::fs::create_dir(&file_path).await?;
    }

    Ok(())
}

async fn verify(install_path: &OsPath, build_manifest_bytes: &[u8]) -> tokio::io::Result<()> {
    let mut thread_handlers = vec![];
    let mut manifest = csv::Reader::from_reader(build_manifest_bytes);

    for record in manifest.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");
        let local_file_path = install_path.join(&record.file_name);

        thread_handlers.push(tokio::spawn(async move {
            if record.flags == 40 {
                // Is directory and doesn't exist
                if !local_file_path.to_path().is_dir() {
                    println!("Warning: {} is not a directory", local_file_path);
                }
                return;
            }

            if !local_file_path.exists() {
                println!("Warning: {} is missing", local_file_path);
                return;
            }

            println!("Verifying {}", &record.file_name);
            match verify_file_hash(&local_file_path, &record.sha) {
                Ok(true) => println!("{} is valid", &record.file_name),
                _ => println!(
                    "Warning: {} does not match the expected signature",
                    local_file_path
                ),
            }
        }));
    }

    for handler in thread_handlers {
        handler.await?;
    }

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
