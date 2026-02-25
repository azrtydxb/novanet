use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Locate the proto file relative to the workspace root.
    // The proto lives at <project_root>/api/v1/novanet.proto.
    let proto_path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap()
        .join("api")
        .join("v1")
        .join("novanet.proto");

    let proto_dir = proto_path.parent().unwrap();

    println!("cargo:rerun-if-changed={}", proto_path.display());

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(&[&proto_path], &[proto_dir])?;

    Ok(())
}
