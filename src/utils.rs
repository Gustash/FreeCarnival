use std::{io::SeekFrom, sync::Arc};

use bytes::Bytes;
use directories::ProjectDirs;
use os_path::OsPath;
use tokio::{
    fs,
    io::{AsyncSeekExt, AsyncWriteExt},
};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
        GalaRequest,
    },
    config::{GalaConfig, LibraryConfig},
    constants::MAX_CHUNK_SIZE,
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

    // Create install directory if it doesn't exist
    fs::create_dir_all(&*install_path).await?;

    println!("Building folder structure...");
    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");

        prepare_file_folder(&install_path, &record.file_name, record.flags).await?;
    }

    println!("Building chunks...");
    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);
    for record in manifest_chunks_rdr.deserialize::<BuildManifestChunksRecord>() {
        let record = record.expect("Failed to deserialize chunks manifest");

        let ChunkDownloadDetails(file_path, file_exists) =
            prepare_chunk_batch_folder(&record, &install_path).await?;

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

            let offset: u64 = *MAX_CHUNK_SIZE * u64::from(record.id);
            println!("Offset: {offset}");

            println!("File path: {}", file_path);
            save_chunk(&file_path, &chunk, offset)
                .await
                .expect(&format!("Failed to save {}.bin", &record.sha));
        }));
    }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    Ok(())
}

struct ChunkDownloadDetails(OsPath, bool);

async fn prepare_chunk_batch_folder(
    record: &BuildManifestChunksRecord,
    base_download_path: &OsPath,
) -> tokio::io::Result<ChunkDownloadDetails> {
    // TODO: Verify chunk SHA
    let file_path = base_download_path.join(&record.file_path);
    let file_exists = file_path.exists();
    Ok(ChunkDownloadDetails(file_path, file_exists))
}

async fn save_chunk(file_path: &OsPath, chunk: &Bytes, offset: u64) -> tokio::io::Result<()> {
    let mut file = tokio::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .open(file_path)
        .await?;
    file.seek(SeekFrom::Start(offset)).await?;
    file.write_all(chunk).await
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
