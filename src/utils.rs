use std::{io::SeekFrom, sync::Arc};

use bytes::Bytes;
use directories::ProjectDirs;
use os_path::OsPath;
use sha2::{Digest, Sha256};
use tokio::{
    fs,
    io::{AsyncSeekExt, AsyncWriteExt},
};

use crate::{
    api::{
        self,
        auth::Product,
        product::{BuildManifestChunksRecord, BuildManifestRecord},
    },
    config::{GalaConfig, LibraryConfig},
    constants::MAX_CHUNK_SIZE,
};

pub(crate) async fn install(client: reqwest::Client, slug: &String) -> Result<(), reqwest::Error> {
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
                build_from_manifest(
                    client_arc,
                    product_arc,
                    build_manifest.as_bytes(),
                    build_manifest_chunks.as_bytes(),
                    &install_path,
                )
                .await
                .expect("Failed to build from manifest");

                println!("Verifying files...");
                verify(&install_path, build_manifest.as_bytes())
                    .await
                    .expect("Failed to verify files");
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
    install_path: &OsPath,
) -> tokio::io::Result<()> {
    let mut thread_handlers = vec![];

    // Create install directory if it doesn't exist
    fs::create_dir_all(&install_path).await?;

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

        // TODO: Verify chunk SHA
        let file_path = install_path.join(&record.file_path);
        let client = client.clone();
        let product = product.clone();
        let record = Arc::new(record);
        thread_handlers.push(tokio::spawn(async move {
            let chunk = api::product::download_chunk(&client, &product, &record.sha)
                .await
                .expect(&format!("Failed to download {}.bin", &record.sha));

            let offset: u64 = *MAX_CHUNK_SIZE * u64::from(record.id);
            save_chunk(&file_path, &chunk, offset)
                .await
                .expect(&format!("Failed to save {}.bin", &record.sha));
        }));
    }

    for handler in thread_handlers {
        handler.await?;
    }

    Ok(())
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
