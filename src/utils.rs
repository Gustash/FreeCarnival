use std::{
    collections::HashMap,
    fmt::format,
    path::{Path, PathBuf},
    sync::Arc,
};

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
                    .data_dir()
                    .join(&product.slugged_name);

                let client_arc = Arc::new(client);
                let product_arc = Arc::new(product.clone());
                let install_path_arc = Arc::new(OsPath::from(&install_path));
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

    // let mut file_name_md5_map = HashMap::new();

    let mut manifest_rdr = csv::Reader::from_reader(build_manifest_bytes);

    let mut manifest_chunks_rdr = csv::Reader::from_reader(build_manifest_chunks_bytes);

    for record in manifest_chunks_rdr.deserialize::<BuildManifestChunksRecord>() {
        let record = record.expect("Failed to deserialize chunks manifest");

        let sha_parts = Arc::new(
            record
                .sha
                .split("_")
                .map(|s| s.to_owned())
                .collect::<Vec<String>>(),
        );
        let file_md5 = &sha_parts[0];
        let download_path = download_path.join(file_md5);
        let download_path_exists = match download_path.try_exists() {
            Ok(exists) => exists,
            Err(_) => false,
        };
        if !download_path_exists {
            fs::create_dir(&download_path).await?;
        }

        let client = client.clone();
        let product = product.clone();
        let record = Arc::new(record);
        let sha_parts = sha_parts.clone();
        thread_handlers.push(tokio::spawn(async move {
            let chunk_idx = &sha_parts[1];
            // TODO: Store chunk SHAs
            let chunk_sha = &sha_parts[2];
            let file_path = download_path.join(format!("{}.bin", chunk_idx));

            let exists = match file_path.try_exists() {
                Ok(exists) => exists,
                Err(_) => false,
            };
            if exists {
                return;
            }

            let chunk = api::product::download_chunk(&client, &product, &record.sha)
                .await
                .unwrap();

            tokio::fs::write(file_path, chunk)
                .await
                .expect(&String::from(format!(
                    "Failed to download {}.bin",
                    record.sha
                )));
        }));
    }

    // for record in manifest_rdr.deserialize::<BuildManifestRecord>() {
    //     let record = record.expect("Failed to deserialize build manifest");
    //     let file_path = install_path.join(&record.file_name);
    //
    //     // File Name is a directory. We should create this directory.
    //     if record.flags == 40 {
    //         if !file_path.exists() {
    //             fs::create_dir(file_path).await?;
    //         }
    //         continue;
    //     }
    //
    //     let chunks = manifest_chunks
    //         .by_ref()
    //         .take(record.chunks)
    //         .map(|c| c.unwrap())
    //         .collect::<Vec<BuildManifestChunksRecord>>();
    //
    //     let client = client.clone();
    //     let product = product.clone();
    //     thread_handlers.push(tokio::spawn(async {
    //         build_file(client, product, chunks, file_path)
    //             .await
    //             .unwrap();
    //     }));
    // }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    // Create install path directory first
    fs::create_dir_all(&*install_path).await?;
    let mut thread_handlers = vec![];

    let download_path = Arc::new(download_path);
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

        let download_path = download_path.clone();
        thread_handlers.push(tokio::spawn(async move {
            for idx in 0..record.chunks {
                let file_md5 = file_name_md5(&record.file_name);
                let chunk_path = download_path.join(file_md5).join(format!("{}.bin", idx));
                let chunk = fs::read(chunk_path).await.expect("Failed to read chunk");
                let mut file = fs::OpenOptions::new()
                    .write(true)
                    // New file. Create it.
                    .create(idx == 0)
                    // File exists. Append contents to existing file.
                    .append(idx != 0)
                    .open(&file_path)
                    .await
                    .unwrap();
                file.write(&chunk).await.expect("Failed to write chunk");
            }
        }));

        // let download_path = download_path_arc.clone();
        // thread_handlers.push(tokio::spawn(async move {
        //     build_file(&download_path, file_path, &record.file_name).await;
        // }));
    }

    for handler in thread_handlers {
        handler.await.unwrap();
    }

    Ok(())
}

async fn build_file(download_path: &PathBuf, file_path: OsPath, file_name: &String) {
    let md5 = file_name_md5(file_name);
    println!("MD5 for {} is {}", file_name, md5);
}

// async fn build_file(
//     client: Arc<reqwest::Client>,
//     product: Arc<Product>,
//     chunks: Vec<BuildManifestChunksRecord>,
//     file_path: OsPath,
// ) -> tokio::io::Result<()> {
//     for chunk_record in chunks {
//         let chunk = api::product::download_chunk(&client, &product, &chunk_record.sha)
//             .await
//             .expect(&String::from(format!(
//                 "Failed to download chunk {}",
//                 chunk_record.sha
//             )));
//
//         println!("Exists ({}): {}", file_path, file_path.exists());
//         // FIXME: Handle errors better in thread
//         let mut file = fs::OpenOptions::new()
//             .write(true)
//             // New file. Create it.
//             .create(!file_path.exists())
//             // File exists. Append contents to existing file.
//             .append(file_path.exists())
//             .open(&file_path)
//             .await
//             .unwrap();
//         file.write(&chunk).await?;
//     }
//
//     Ok(())
// }

fn file_name_md5(file_name: &String) -> String {
    let mut file_path_md5 = Md5::new();
    file_path_md5.input_str(file_name);
    file_path_md5.result_str()
}
