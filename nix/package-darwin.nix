# The prebuilt Wimy.app from a GitHub release, signed in CI with the
# wimy-dev identity (see contrib/macos/SIGNING.md). Nothing here may
# modify the bundle: that would break its signature, and with it the
# Accessibility permission.
{
  lib,
  stdenvNoCC,
  fetchurl,
  unzip,
  release,
}:
stdenvNoCC.mkDerivation {
  pname = "wimy";
  inherit (release) version;
  src = fetchurl { inherit (release) url hash; };
  nativeBuildInputs = [ unzip ];
  sourceRoot = ".";
  dontConfigure = true;
  dontBuild = true;
  dontFixup = true; # no stripping or patching of the signed binaries
  installPhase = ''
    runHook preInstall
    # AppleDouble files (._*) from an archive made with extended
    # attributes are not part of the bundle; left in, they break its seal
    find Wimy.app -name '._*' -delete
    mkdir -p $out/Applications $out/bin
    cp -R Wimy.app $out/Applications/
    ln -s $out/Applications/Wimy.app/Contents/MacOS/wimyctl $out/bin/wimyctl
    runHook postInstall
  '';
  meta = {
    description = "wmii-style tiling window manager for macOS (signed release build)";
    homepage = "https://github.com/jaym/wimy";
    platforms = [ "aarch64-darwin" ];
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
  };
}
