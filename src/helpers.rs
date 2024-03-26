use std::{
    collections::{HashMap, HashSet},
    path::PathBuf,
    sync::Arc,
};

use async_recursion::async_recursion;
use bytes::Bytes;
use directories::ProjectDirs;
use indicatif::{MultiProgress, ProgressBar, ProgressStyle};
use os_path::OsPath;
use queues::{queue, IsQueue, Queue};
use sha2::{Digest, Sha256};
use tokio::{
    fs::File,
    io::AsyncWriteExt,
    sync::{OwnedSemaphorePermit, Semaphore},
};

use crate::{
    api,
    cli::InstallOpts,
    constants::{MAX_CHUNK_SIZE, PROJECT_NAME},
    shared::models::{
        api::{BuildOs, Product},
        BuildManifestChunksRecord, BuildManifestRecord, ChangeTag,
    },
};

#[async_recursion]
pub(crate) async fn find_exe_recursive(path: &PathBuf) -> Option<PathBuf> {
    let mut subdirs = vec![];
    let mut exes = vec![];

    match tokio::fs::read_dir(path).await {
        Ok(mut subpath) => {
            while let Ok(Some(entry)) = subpath.next_entry().await {
                let entry_path = entry.path();
                if entry_path.is_dir() {
                    subdirs.push(entry_path);
                    continue;
                }

                if entry_path.is_file() {
                    // Check if the current path is a file with a .exe extension
                    println!("Checking file: {}", entry_path.display());
                    if let (Some(ext), Some(file_name)) =
                        (entry_path.extension(), entry_path.file_name())
                    {
                        let file_name_str = match file_name.to_str() {
                            Some(str) => str.to_lowercase(),
                            None => String::new(),
                        };
                        if ext == "exe"
                            && !file_name_str.contains("setup")
                            && !file_name_str.contains("unins")
                        {
                            exes.push(entry_path);
                        }
                    }
                }
            }
        }
        Err(err) => {
            println!("Failed to iterate over {}: {:?}", path.display(), err);
        }
    }

    if !exes.is_empty() {
        exes.sort();
        return Some(exes.swap_remove(0));
    }

    for dir in subdirs {
        println!("Checking directory: {}", dir.display());
        if let Some(exe_path) = find_exe_recursive(&dir.to_path_buf()).await {
            return Some(exe_path);
        }
    }

    None
}

pub(crate) async fn read_or_generate_delta_manifest(
    slug: &String,
    old_manifest_bytes: &[u8],
    new_manifest_bytes: &[u8],
    old_version: &String,
    new_version: &String,
) -> tokio::io::Result<Vec<u8>> {
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
        .byte_records()
        .map(|r| {
            let mut record = r.expect("Failed to get byte record");
            record.push_field(b"");
            record
                .deserialize::<BuildManifestRecord>(None)
                .expect("Failed to deserialize updated build manifest")
        })
        .collect();
    let mut old_manifest_rdr = csv::Reader::from_reader(old_manifest_bytes);
    let old_manifest_iter: Vec<BuildManifestRecord> = old_manifest_rdr
        .byte_records()
        .map(|r| {
            let mut record = r.expect("Failed to get byte record");
            record.push_field(b"");
            record
                .deserialize::<BuildManifestRecord>(None)
                .expect("Failed to deserialize old build manifest")
        })
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
            build_manifest_delta_wtr
                .serialize(BuildManifestRecord {
                    tag: Some(ChangeTag::Removed),
                    ..old_entry
                })
                .expect("Failed to serialize delta build manifest");
        }
    }
    let delta_bytes = build_manifest_delta_wtr.into_inner().unwrap();
    store_build_manifest(
        &delta_bytes,
        &format!("{}_{}", old_version, new_version),
        slug,
        "manifest_delta",
    )
    .await?;

    Ok(delta_bytes)
}

pub(crate) async fn read_or_generate_delta_chunks_manifest(
    slug: &String,
    delta_manifest_bytes: &[u8],
    new_manifest_bytes: &[u8],
    old_version: &String,
    new_version: &String,
) -> tokio::io::Result<Vec<u8>> {
    let manifest_delta_version = format!("{}_{}", old_version, new_version);
    if let Ok(exising_delta) =
        read_build_manifest(&manifest_delta_version, slug, "manifest_delta_chunks").await
    {
        println!("Using existing chunks delta manifest");
        return Ok(exising_delta);
    }

    println!("Generating chunks delta manifest...");
    let mut delta_manifest_rdr = csv::Reader::from_reader(delta_manifest_bytes);
    let mut delta_manifest = delta_manifest_rdr.byte_records().map(|r| {
        let record = r.expect("Failed to get byte record");
        record.deserialize::<BuildManifestRecord>(None)
    });
    let mut current_file = delta_manifest
        .next()
        .expect("Failed to deserialize build manifest delta")
        .expect("There were no changes in this update?");

    let mut new_manifest_rdr = csv::Reader::from_reader(new_manifest_bytes);
    let new_manifest_byte_records = new_manifest_rdr.byte_records();
    let mut build_manifest_delta_wtr = csv::Writer::from_writer(vec![]);

    for record in new_manifest_byte_records {
        let record = record.expect("Failed to get byte record");
        let record = record
            .deserialize::<BuildManifestChunksRecord>(None)
            .expect("Failed to deserialize build manifest chunks");

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

        if record.file_path != current_file.file_name {
            continue;
        }

        build_manifest_delta_wtr
            .serialize(&record)
            .expect("Failed to serialize build manifest chunks");

        if usize::from(record.id) + 1 == current_file.chunks {
            println!("Done processing chunks for {}", record.file_path);
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

    let delta_bytes = build_manifest_delta_wtr.into_inner().unwrap();
    store_build_manifest(
        &delta_bytes,
        &format!("{}_{}", old_version, new_version),
        slug,
        "manifest_delta_chunks",
    )
    .await?;

    Ok(delta_bytes)
}

pub(crate) async fn store_build_manifest(
    body: &[u8],
    build_number: &String,
    product_slug: &String,
    file_suffix: &str,
) -> tokio::io::Result<()> {
    let project = ProjectDirs::from("rs", "", *PROJECT_NAME).unwrap();
    let path = project.config_dir().join("manifests").join(product_slug);
    tokio::fs::create_dir_all(&path).await?;

    let path = path.join(format!("{}_{}.csv", build_number, file_suffix));
    tokio::fs::write(path, body).await
}

pub(crate) async fn read_build_manifest(
    build_number: &String,
    product_slug: &String,
    file_suffix: &str,
) -> tokio::io::Result<Vec<u8>> {
    let project = ProjectDirs::from("rs", "", *PROJECT_NAME).unwrap();
    let path = project
        .config_dir()
        .join("manifests")
        .join(product_slug)
        .join(format!("{}_{}.csv", build_number, file_suffix));
    tokio::fs::read(path).await
}

pub(crate) async fn build_from_manifest(
    client: reqwest::Client,
    product: Arc<Product>,
    os: Arc<BuildOs>,
    build_manifest_bytes: &[u8],
    build_manifest_chunks_bytes: &[u8],
    install_path: OsPath,
    install_opts: InstallOpts,
) -> tokio::io::Result<bool> {
    let mut write_queue = queue![];
    let mut chunk_queue = queue![];

    // Create install directory if it doesn't exist
    tokio::fs::create_dir_all(&install_path).await?;

    let mut file_chunk_num_map = HashMap::new();
    let mut total_bytes = 0u64;

    let m = MultiProgress::new();

    println!("Building folder structure...");
    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    let byte_records = manifest_rdr.byte_records();
    #[cfg(target_os = "macos")]
    let mut mac_app = mac::MacAppExecutables::new();

    for record in byte_records {
        let mut record = record.expect("Failed to get byte record");
        if record.get(5).is_none() {
            record.push_field(b"");
        }
        let record = record
            .deserialize::<BuildManifestRecord>(None)
            .expect("Failed to deserialize build manifest");

        if record.tag == Some(ChangeTag::Modified) || record.tag == Some(ChangeTag::Removed) {
            let file_path = install_path.join(&record.file_name);
            println!("Removing {}", file_path);
            if record.is_directory() {
                println!("{} is a directory", file_path);
                // Is a directory
                if file_path.exists() && file_path.to_path().is_dir() {
                    println!("Deleting {}", file_path);
                    // Delete this directory
                    tokio::fs::remove_dir_all(file_path).await?;
                }
                continue;
            }

            println!("{} is a file", file_path);
            if file_path.exists() && file_path.is_file() {
                println!("Deleting {}", file_path);
                // Delete this file
                tokio::fs::remove_file(file_path).await?;
            }

            if record.tag == Some(ChangeTag::Removed) {
                continue;
            }
        }

        prepare_file(
            &install_path,
            #[cfg(target_os = "macos")]
            &os,
            &record.file_name,
            record.is_directory(),
            #[cfg(target_os = "macos")]
            &mut mac_app,
        )
        .await?;

        if !record.is_directory() {
            file_chunk_num_map.insert(record.file_name.clone(), record.chunks);
            total_bytes += record.size_in_bytes as u64;
        }
    }

    let dl_sty =
        ProgressStyle::with_template("{wide_msg} Download: {binary_bytes_per_sec}").unwrap();
    let wr_sty = ProgressStyle::with_template(
        "{wide_msg} Disk: {binary_bytes_per_sec}\n[{percent}%] {wide_bar} {bytes:>7}/{total_bytes:7} [{eta_precise}]",
    )
    .unwrap()
    .progress_chars("##-");

    let dl_prog = Arc::new(m.add(ProgressBar::new(total_bytes).with_style(dl_sty)));
    let wrt_prog =
        Arc::new(m.insert_after(&dl_prog, ProgressBar::new(total_bytes).with_style(wr_sty)));

    println!("Building queue...");
    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);
    let byte_records = manifest_chunks_rdr.byte_records();
    for record in byte_records {
        let record = record.expect("Failed to get byte record");
        let record = record
            .deserialize::<BuildManifestChunksRecord>(None)
            .expect("Failed to deserialize chunks manifest");

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
        async_channel::unbounded::<(BuildManifestChunksRecord, Bytes, OwnedSemaphorePermit)>();

    println!("Spawning write thread...");
    let write_handler = tokio::spawn(async move {
        println!("Write thread started.");

        let mut in_buffer = HashMap::new();
        let mut file_map = HashMap::new();

        while write_queue.size() > 0 {
            let (record, chunk, permit) = match rx.recv().await {
                Ok(msg) => msg,
                Err(_) => {
                    println!("Write channel has closed");
                    break;
                }
            };

            // Some files don't have the chunk id in the sha parts, so they can have reused
            // SHAs for chunks (e.g. DieYoungPrologue-WindowsNoEditor.pak)
            let chunk_key = format!("{},{}", record.id, record.sha);
            in_buffer.insert(chunk_key, (record.file_path, chunk, permit));

            loop {
                match write_queue.peek() {
                    Ok((next_chunk, chunk_id, is_last_chunk)) => {
                        let next_chunk_key = format!("{},{}", chunk_id, next_chunk);
                        if let Some((file_path, bytes, permit)) = in_buffer.remove(&next_chunk_key)
                        {
                            if !file_map.contains_key(&file_path) {
                                let chunk_file_path = install_path.join(&file_path);
                                let file = open_file(&chunk_file_path).await.unwrap_or_else(|_| {
                                    panic!("Failed to open {}", chunk_file_path)
                                });
                                file_map.insert(file_path.clone(), file);
                            }
                            let file = file_map.get_mut(&file_path).unwrap();
                            write_queue.remove().unwrap();
                            // println!("Writing {}", next_chunk);
                            let bytes_written = bytes.len();
                            append_chunk(file, bytes).await.unwrap_or_else(|_| {
                                panic!("Failed to write {}.bin to {}", next_chunk, file_path)
                            });
                            drop(permit);

                            wrt_prog.inc(bytes_written as u64);

                            if is_last_chunk {
                                file_map.remove(&file_path);
                            }

                            continue;
                        }

                        // println!(
                        //     "Not ready to write {}: {} pending",
                        //     next_chunk,
                        //     in_buffer.len()
                        // );

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
    let max_chunks_in_memory = install_opts.max_memory_usage / *MAX_CHUNK_SIZE;
    let mem_semaphore = Arc::new(Semaphore::new(max_chunks_in_memory));
    let dl_semaphore = Arc::new(Semaphore::new(install_opts.max_download_workers));
    while let Ok(record) = chunk_queue.remove() {
        let mem_permit = mem_semaphore.clone().acquire_owned().await.unwrap();
        let client = client.clone();
        let product = product.clone();
        let os = os.clone();
        let thread_tx = tx.clone();
        let dl_prog = dl_prog.clone();
        let dl_semaphore = dl_semaphore.clone();

        tokio::spawn(async move {
            // println!("Downloading {}", record.sha);
            let dl_permit = dl_semaphore.acquire().await.unwrap();
            let chunk = api::product::download_chunk(&client, &product, &os, &record.sha)
                .await
                .unwrap_or_else(|_| panic!("Failed to download {}.bin", &record.sha));
            drop(dl_permit);

            dl_prog.inc(chunk.len() as u64);

            if !install_opts.skip_verify {
                let chunk_parts = &record.sha.split('_').collect::<Vec<&str>>();
                match chunk_parts.last() {
                    Some(chunk_sha) => {
                        // println!("Verifying {}", record.sha);
                        let chunk_corrupted = !verify_chunk(&chunk, chunk_sha);

                        if chunk_corrupted {
                            println!("Sha: {}", chunk_sha);
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

            thread_tx.send((record, chunk, mem_permit)).await.unwrap();

            true
        });
    }

    println!("Waiting for write thread to finish...");
    write_handler.await?;

    #[cfg(target_os = "macos")]
    if *os == BuildOs::Mac {
        mac_app.mark_as_executable().await?;
    }

    // TODO: Redo logic for verification
    Ok(true)
}

pub(crate) async fn open_file(file_path: &OsPath) -> tokio::io::Result<File> {
    tokio::fs::OpenOptions::new()
        .append(true)
        .open(file_path)
        .await
}

pub(crate) async fn append_chunk(
    file: &mut tokio::fs::File,
    chunk: Bytes,
) -> tokio::io::Result<()> {
    file.write_all(&chunk).await
}

pub(crate) async fn prepare_file(
    base_install_path: &OsPath,
    #[cfg(target_os = "macos")] os: &BuildOs,
    file_name: &String,
    is_directory: bool,
    #[cfg(target_os = "macos")] mac_executable: &mut mac::MacAppExecutables,
) -> tokio::io::Result<()> {
    let file_path = base_install_path.join(file_name);

    // File is a directory. We should create this directory.
    if is_directory {
        if !file_path.exists() {
            tokio::fs::create_dir(&file_path).await?;
        }
    } else {
        // Create empty file.
        tokio::fs::File::create(&file_path).await?;
    }

    #[cfg(target_os = "macos")]
    if os == &BuildOs::Mac && mac_executable.plist.is_none() {
        if let Some(ext) = file_path.extension() {
            if &ext == "app" {
                let plist = mac::find_info_plist(&file_path.to_pathbuf());
                mac_executable.set_plist(plist);
            }
        };
    }

    Ok(())
}

pub(crate) fn verify_file_hash(file_path: &OsPath, sha: &str) -> std::io::Result<bool> {
    let mut file = std::fs::File::open(file_path)?;
    let mut hasher = Sha256::new();
    std::io::copy(&mut file, &mut hasher)?;
    let hash = hasher.finalize();
    let file_sha = base16ct::lower::encode_string(&hash);

    Ok(file_sha == sha)
}

pub(crate) fn verify_chunk(chunk: &Bytes, sha: &str) -> bool {
    let mut hasher = Sha256::new();
    hasher.update(chunk);
    let hash = hasher.finalize();
    let sha_str = base16ct::lower::encode_string(&hash);

    sha_str == sha
}

#[cfg(target_os = "macos")]
pub(crate) mod mac {
    use std::path::{Path, PathBuf};

    use async_recursion::async_recursion;
    use serde::Deserialize;

    #[async_recursion]
    pub(crate) async fn find_app_recursive(path: &PathBuf) -> Option<PathBuf> {
        let mut subdirs = vec![];

        match tokio::fs::read_dir(path).await {
            Ok(mut subpath) => {
                while let Ok(Some(entry)) = subpath.next_entry().await {
                    let entry_path = entry.path();
                    // Check if the current path is a .app extension
                    println!("Checking file: {}", entry_path.display());
                    if let Some(ext) = entry_path.extension() {
                        if ext == "app" {
                            return Some(entry_path);
                        }
                    }

                    if entry_path.is_dir() {
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
            if let Some(app_path) = find_app_recursive(&dir.to_path_buf()).await {
                return Some(app_path);
            }
        }

        None
    }

    pub(crate) struct MacAppExecutables {
        pub(crate) plist: Option<PathBuf>,
    }

    #[derive(Deserialize)]
    struct BasicInfoPlist {
        #[serde(rename = "CFBundleExecutable")]
        bundle_executable: String,
    }

    impl MacAppExecutables {
        pub(crate) fn new() -> Self {
            Self { plist: None }
        }

        pub(crate) fn with_plist(plist: PathBuf) -> Self {
            Self { plist: Some(plist) }
        }

        pub(crate) fn set_plist(&mut self, plist: PathBuf) {
            self.plist = Some(plist);
        }

        pub(crate) fn executable(&self) -> Option<PathBuf> {
            match &self.plist {
                Some(plist_path) => {
                    let plist: BasicInfoPlist = plist::from_file(plist_path).unwrap();
                    let executable_path = plist_path
                        .parent()
                        .unwrap()
                        .join("MacOS")
                        .join(plist.bundle_executable);

                    Some(executable_path)
                }
                None => None,
            }
        }

        pub(crate) async fn mark_as_executable(&self) -> tokio::io::Result<()> {
            use std::{fs::Permissions, os::unix::prelude::PermissionsExt};

            match &self.executable() {
                Some(executable_path) => {
                    let permissions: Permissions = PermissionsExt::from_mode(0o755); // Read/write/execute
                    tokio::fs::set_permissions(executable_path, permissions).await?;
                }
                None => {
                    println!("No executable set, cannot mark as executable.");
                }
            };

            Ok(())
        }
    }

    pub(crate) fn find_info_plist(app_path: &Path) -> PathBuf {
        app_path.join("Contents").join("Info.plist")
    }
}
