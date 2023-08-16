use crate::{
    api,
    config::{GalaConfig, LibraryConfig},
};

pub(crate) async fn install(slug: &String) -> Result<(), reqwest::Error> {
    let library = LibraryConfig::load().expect("Failed to load library");
    let product = library
        .collection
        .iter()
        .find(|p| p.slugged_name == slug.to_owned());

    if let Some(product) = product {
        println!("Found game");
        return match api::product::get_latest_build_number(&product).await? {
            Some(build_version) => {
                api::product::get_build_manifest(&product, &build_version).await;
                Ok(())
            }
            None => Ok(()),
        };
    }

    println!("Could not find {slug} in library");
    Ok(())
}
