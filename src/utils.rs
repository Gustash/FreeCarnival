use std::{path::PathBuf, sync::Arc};

use bytes::Bytes;
use crypto::{digest::Digest, md5::Md5};
use directories::ProjectDirs;
use os_path::OsPath;
use tokio::{fs, io::AsyncWriteExt};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
        GalaRequest,
    },
    config::{GalaConfig, LibraryConfig},
};

pub(crate) async fn install(slug: &String) -> Result<(), reqwest::Error> {
    let client = GalaRequest::new().client;
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

                let client_arc = Arc::new(client);
                let product_arc = Arc::new(product.clone());

                println!("Fetching build manifest chunks...");
                let build_manifest_chunks = api::product::get_build_manifest_chunks(
                    client_arc.clone(),
                    product_arc.clone(),
                    &build_version,
                )
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

                let install_path_arc = Arc::new(OsPath::from(&install_path));
                println!("Installing game from manifest...");
                build_from_manifest(
                    client_arc,
                    product_arc,
                    build_manifest.as_bytes(),
                    build_manifest_chunks.as_bytes(),
                    install_path_arc,
                )
                .await
                .expect("Failed to build from manifest");
                Ok(())
            }
            None => Ok(()),
        };
    }

    println!("Could not find {slug} in library");
    Ok(())
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
    install_path: Arc<OsPath>,
) -> tokio::io::Result<()> {
    let mut thread_handlers = vec![];
    let project = ProjectDirs::from("rs", "", "openGala").unwrap();
    let cache_path = project.cache_dir();
    let download_path = cache_path.join(&product.slugged_name);
    // Create cache path if it doesn't exist
    fs::create_dir_all(&*download_path).await?;

    println!("Downloading chunks...");
    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);
    for record in manifest_chunks_rdr.deserialize::<BuildManifestChunksRecord>() {
        let record = record.expect("Failed to deserialize chunks manifest");

        let ChunkDownloadDetails(file_path, file_exists) =
            prepare_chunk_batch_folder(&record, &download_path).await?;

        if file_exists {
            continue;
        }

        let client = client.clone();
        let product = product.clone();
        let record = Arc::new(record);
        thread_handlers.push(tokio::spawn(async move {
            let chunk = api::product::download_chunk(&client, &product, &record.sha)
                .await
                .expect(&format!("Failed to download {}.bin", &record.sha));

            println!("File path: {}", file_path.display());
            save_chunk(&file_path, &chunk)
                .await
                .expect(&format!("Failed to save {}.bin", &record.sha));
        }));
    }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    // Create install path directory first
    fs::create_dir_all(&*install_path).await?;
    let mut thread_handlers = vec![];
    let download_path = Arc::new(download_path);

    println!("Building files...");
    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");
        let FileDownloadDetails(file_path, is_directory) =
            prepare_file_folder(&install_path, &record.file_name, record.flags)
                .await
                .expect(&format!(
                    "Failed to prepare install location for {}",
                    &record.file_name
                ));

        if is_directory {
            continue;
        }

        let download_path = download_path.clone();
        thread_handlers.push(tokio::spawn(async move {
            build_file(&file_path, &download_path, record.chunks, &record.file_name)
                .await
                .expect(&format!(
                    "Failed to build {} from chunks",
                    &record.file_name
                ));
        }));
    }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    // Delete download dir after install
    if let Err(_) = tokio::fs::remove_dir_all(&*download_path).await {
        println!(
            "Failed to delete temporary download folder: {}\n\nYou may want to delete it manually.",
            &download_path.display()
        );
    };

    Ok(())
}

struct ChunkDownloadDetails(PathBuf, bool);

async fn prepare_chunk_batch_folder(
    record: &BuildManifestChunksRecord,
    base_download_path: &PathBuf,
) -> tokio::io::Result<ChunkDownloadDetails> {
    // TODO: Save chunk SHA
    let sha_parts = record
        .sha
        .split("_")
        .map(|s| s.to_owned())
        .collect::<Vec<String>>();
    let file_md5 = &sha_parts[0];
    let chunk_idx = &sha_parts[1];

    let download_path = base_download_path.join(file_md5);
    let download_path_exists = match download_path.try_exists() {
        Ok(exists) => exists,
        Err(_) => false,
    };
    if !download_path_exists {
        fs::create_dir(&download_path).await?;
    }

    let file_path = download_path.join(format!("{}.bin", chunk_idx));
    let file_exists = match file_path.try_exists() {
        Ok(exists) => exists,
        Err(_) => false,
    };

    Ok(ChunkDownloadDetails(file_path, file_exists))
}

async fn save_chunk(file_path: &PathBuf, chunk: &Bytes) -> tokio::io::Result<()> {
    tokio::fs::write(file_path, chunk).await
}

struct FileDownloadDetails(OsPath, bool);

async fn prepare_file_folder(
    base_install_path: &OsPath,
    file_name: &String,
    flags: u8,
) -> tokio::io::Result<FileDownloadDetails> {
    let file_path = base_install_path.join(file_name);
    let is_directory = flags == 40;

    // File Name is a directory. We should create this directory.
    if is_directory && !file_path.exists() {
        fs::create_dir(&file_path).await?;
    }

    Ok(FileDownloadDetails(file_path, is_directory))
}

async fn build_file(
    file_path: &OsPath,
    download_path: &PathBuf,
    num_of_chunks: usize,
    file_name: &String,
) -> tokio::io::Result<()> {
    for idx in 0..num_of_chunks {
        let file_md5 = file_name_md5(file_name);
        let chunk_path = download_path.join(file_md5).join(format!("{}.bin", idx));
        let chunk = fs::read(chunk_path).await.expect("Failed to read chunk");
        let mut file = fs::OpenOptions::new()
            .write(true)
            // New file. Create it.
            .create(idx == 0)
            // File exists. Append contents to existing file.
            .append(idx != 0)
            .open(&file_path)
            .await?;
        file.write(&chunk).await?;
    }

    Ok(())
}

fn file_name_md5(file_name: &String) -> String {
    let mut file_path_md5 = Md5::new();
    file_path_md5.input_str(file_name);
    file_path_md5.result_str()
}
