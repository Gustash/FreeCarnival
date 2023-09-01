use std::{collections::HashMap, sync::Arc};

use bytes::Bytes;
use directories::ProjectDirs;
use os_path::OsPath;
use queues::*;
use sha2::{Digest, Sha256};
use tokio::{
    fs::{self, File},
    io::AsyncWriteExt,
    sync::Mutex,
    task::JoinHandle,
};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
    },
    config::{GalaConfig, LibraryConfig},
};

pub(crate) async fn install(
    client: reqwest::Client,
    slug: &String,
) -> Result<bool, reqwest::Error> {
    let library = LibraryConfig::load().expect("Failed to load library");
    let product = library
        .collection
        .iter()
        .find(|p| p.slugged_name == slug.to_owned());

    if let Some(product) = product {
        println!("Found game. Fetching latest version build number...");
        return match api::product::get_latest_build_number(&client, &product).await? {
            Some(build_version) => {
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
                    api::product::get_build_manifest_chunks(&client, &product, &build_version)
                        .await?;
                store_build_manifest(
                    &build_manifest_chunks,
                    &build_version,
                    &product.slugged_name,
                    "manifest_chunks",
                )
                .await
                .expect("Failed to save build manifest chunks");

                let install_path = ProjectDirs::from("rs", "", "openGala")
                    .unwrap()
                    .data_dir()
                    .join(&product.slugged_name);
                let install_path = OsPath::from(install_path);
                let client_arc = Arc::new(client);
                let product_arc = Arc::new(product.clone());

                println!("Installing game from manifest...");
                let result = build_from_manifest(
                    client_arc,
                    product_arc,
                    build_manifest.as_bytes(),
                    build_manifest_chunks.as_bytes(),
                    &install_path,
                )
                .await
                .expect("Failed to build from manifest");

                Ok(result)
            }
            None => Ok(false),
        };
    }

    println!("Could not find {slug} in library");
    Ok(false)
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
    fs::create_dir_all(&path).await?;

    let path = path.join(format!("{}_{}.csv", build_number, file_suffix));
    fs::write(path, body.as_bytes()).await
}

async fn build_from_manifest(
    client: Arc<reqwest::Client>,
    product: Arc<Product>,
    build_manifest_bytes: &[u8],
    build_manifest_chunks_bytes: &[u8],
    install_path: &OsPath,
) -> tokio::io::Result<bool> {
    let mut thread_handlers: Vec<JoinHandle<bool>> = vec![];
    let mut write_queue = queue![];
    let mut chunk_queue = queue![];

    // Create install directory if it doesn't exist
    fs::create_dir_all(&install_path).await?;

    let mut file_chunk_num_map = HashMap::new();

    println!("Building folder structure...");
    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");

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
        write_queue.add((record.sha.clone(), is_last)).unwrap();
        chunk_queue.add(record).unwrap();
    }
    drop(file_chunk_num_map);

    let max_threads = 1024; // TODO: Make variable
    let (tx, rx) =
        crossbeam_channel::bounded::<(BuildManifestChunksRecord, Bytes)>(write_queue.size());
    let (ready_tx, ready_rx) = crossbeam_channel::bounded(max_threads);

    let install_path = install_path.clone();
    println!("Spawning write thread...");
    let write_handler = tokio::spawn(async move {
        let mut in_buffer = HashMap::new();
        let mut file_map = HashMap::new();
        for (record, chunk) in rx {
            in_buffer.insert(record.sha.clone(), (record.file_path.clone(), chunk));

            loop {
                match write_queue.peek() {
                    Ok((next_chunk, is_last_chunk)) => {
                        if let Some((file_path, bytes)) = in_buffer.remove(&next_chunk) {
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

                            // Notify download threads that a download is ready
                            ready_tx.send(()).unwrap();
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
                        break;
                    }
                }
            }
        }
    });

    {
        println!("Downloading chunks...");
        let chunk_queue_arc = Arc::new(Mutex::new(chunk_queue));
        let mut active_threads = 0;
        loop {
            let client = client.clone();
            let product = product.clone();
            let queue = chunk_queue_arc.clone();
            let ready_rx = ready_rx.clone();
            let thread_tx = tx.clone();
            active_threads += 1;
            let record = {
                let mut queue = queue.lock().await;
                match queue.remove() {
                    Ok(record) => record,
                    Err(_) => break,
                }
            };
            thread_handlers.push(tokio::spawn(async move {
                let chunk = api::product::download_chunk(&client, &product, &record.sha)
                    .await
                    .expect(&format!("Failed to download {}.bin", &record.sha));

                let chunk_sha = &record.sha.split("_").collect::<Vec<&str>>()[2];
                let chunk_corrupted = !verify_chunk(&chunk, chunk_sha);

                if chunk_corrupted {
                    println!(
                        "{} failed verification. {} is corrupted.",
                        &record.sha, &record.file_path
                    );
                    return false;
                }

                thread_tx.send((record, chunk)).unwrap();

                true
            }));

            if active_threads >= max_threads {
                match ready_rx.recv() {
                    Ok(()) => {
                        active_threads -= 1;
                    }
                    Err(_) => {
                        println!("Channel disconnected");
                        break;
                    }
                }
            }
        }
    }
    drop(tx);

    println!("Waiting for download threads to finish...");
    let mut result = true;
    for handler in thread_handlers {
        if !handler.await? {
            result = false;
        };
    }
    println!("Waiting for write thread to finish...");
    write_handler.await?;

    Ok(result)
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
        fs::create_dir(&file_path).await?;
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
