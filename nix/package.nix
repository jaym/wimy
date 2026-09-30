# wimy built from source: the river backend on Linux (pure Go), the
# macOS backend on darwin (cgo; ad-hoc signed, so every changed build
# needs the Accessibility permission approved again — prefer the signed
# release, ./package-darwin.nix).
{
  lib,
  stdenv,
  buildGoModule,
  version,
  src,
}:
buildGoModule {
  pname = "wimy";
  inherit version src;
  vendorHash = "sha256-c4HJ0IT1jelrs8WhRwVyoOQ9w2bkqAmqyBsNQIggJOo=";
  subPackages = [
    "cmd/wimy"
    "cmd/wimyctl"
  ];
  env.CGO_ENABLED = if stdenv.hostPlatform.isDarwin then "1" else "0";
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];
  # the unit tests run in CI; the e2e suites need a compositor
  doCheck = false;
  meta = {
    description = "wmii-style tiling window manager for river (Linux) and macOS";
    homepage = "https://github.com/jaym/wimy";
    mainProgram = "wimy";
    platforms = lib.platforms.linux ++ lib.platforms.darwin;
  };
}
