use std::sync::Arc;

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
        println!("Found game");
        return match api::product::get_latest_build_number(&client, &product).await? {
            Some(build_version) => {
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
                    .cache_dir()
                    .join(&product.slugged_name);

                let client_arc = Arc::new(client);
                let product_arc = Arc::new(product.clone());
                build_from_manifest(
                    client_arc,
                    product_arc,
                    build_manifest.as_bytes(),
                    build_manifest_chunks.as_bytes(),
                    OsPath::from(&install_path),
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
    install_path: OsPath,
) -> tokio::io::Result<()> {
    let mut thread_handlers = vec![];
    // TODO: Multi-threaded download
    // Create install path directory first
    fs::create_dir_all(&install_path).await?;

    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);
    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);
    let mut manifest_chunks = manifest_chunks_rdr.deserialize::<BuildManifestChunksRecord>();

    for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
        let record = record.expect("Failed to deserialize build manifest");
        let file_path = install_path.join(&record.file_name);

        // File Name is a directory. We should create this directory.
        if record.flags == 40 {
            if !file_path.exists() {
                fs::create_dir(file_path).await?;
            }
            continue;
        }

        let chunks = manifest_chunks
            .by_ref()
            .take(record.chunks)
            .map(|c| c.unwrap())
            .collect::<Vec<BuildManifestChunksRecord>>();

        let client = client.clone();
        let product = product.clone();
        thread_handlers.push(tokio::spawn(async {
            build_file(client, product, chunks, file_path)
                .await
                .unwrap();
        }));
    }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    Ok(())
}

async fn build_file(
    client: Arc<reqwest::Client>,
    product: Arc<Product>,
    chunks: Vec<BuildManifestChunksRecord>,
    file_path: OsPath,
) -> tokio::io::Result<()> {
    for chunk_record in chunks {
        let chunk = api::product::download_chunk(&client, &product, &chunk_record.sha)
            .await
            .expect(&String::from(format!(
                "Failed to download chunk {}",
                chunk_record.sha
            )));

        println!("Exists ({}): {}", file_path, file_path.exists());
        // FIXME: Handle errors better in thread
        let mut file = fs::OpenOptions::new()
            .write(true)
            // New file. Create it.
            .create(!file_path.exists())
            // File exists. Append contents to existing file.
            .append(file_path.exists())
            .open(&file_path)
            .await
            .unwrap();
        file.write(&chunk).await?;
    }

    Ok(())
}
