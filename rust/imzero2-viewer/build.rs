fn main() {
    const ROOT: &str = "../../proto";
    const FILE: &str = "boxer/imzero2/v1/input.proto";
    println!("cargo:rerun-if-changed={ROOT}/{FILE}");
    let descriptor = protox::compile([FILE], [ROOT]).expect("compile canonical viewer protocol");
    prost_build::Config::new()
        .compile_fds(descriptor)
        .expect("generate viewer protobuf bindings");
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") {
        let prefix = std::env::var("FFMPEG_DIR")
            .expect("FFMPEG_DIR must point to Windows FFmpeg development files");
        println!("cargo:rerun-if-env-changed=FFMPEG_DIR");
        bindgen::Builder::default()
            .header(format!("{prefix}/include/libavutil/hwcontext_d3d11va.h"))
            .clang_arg(format!("-I{prefix}/include"))
            .allowlist_type("AVD3D11VADeviceContext")
            .allowlist_recursively(false)
            .blocklist_type("ID3D.*")
            .layout_tests(false)
            .generate()
            .expect("generate D3D11 FFmpeg hardware context")
            .write_to_file(
                std::path::PathBuf::from(std::env::var_os("OUT_DIR").unwrap())
                    .join("d3d11_context.rs"),
            )
            .expect("write D3D11 context bindings");
    }
}
