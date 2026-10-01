{
  description = "wimy — a wmii-style tiling window manager for river (Linux) and macOS";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    # only for the module's flake check
    home-manager.url = "github:nix-community/home-manager";
    home-manager.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    {
      self,
      nixpkgs,
      home-manager,
    }:
    let
      lib = nixpkgs.lib;
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      eachSystem = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      release = lib.importJSON ./nix/release.json;
      srcVersion = "dev-${self.shortRev or self.dirtyShortRev or "unknown"}";
    in
    {
      packages = eachSystem (
        pkgs:
        let
          fromSource = pkgs.callPackage ./nix/package.nix {
            version = srcVersion;
            src = lib.cleanSource ./.;
          };
        in
        if pkgs.stdenv.hostPlatform.isDarwin then
          {
            # ad-hoc signed: needs the Accessibility permission re-approved per change
            wimy-src = fromSource;
          }
          # the signed release, once one is published (nix/update-release.sh)
          // lib.optionalAttrs (release.version != "") {
            wimy = pkgs.callPackage ./nix/package-darwin.nix { inherit release; };
            default = self.packages.${pkgs.stdenv.hostPlatform.system}.wimy;
          }
        else
          {
            wimy = fromSource;
            default = fromSource;
          }
      );

      homeManagerModules.wimy = import ./nix/hm-module.nix self;
      homeManagerModules.default = self.homeManagerModules.wimy;

      checks = eachSystem (
        pkgs:
        let
          hmCheck = home-manager.lib.homeManagerConfiguration {
            inherit pkgs;
            modules = [
              self.homeManagerModules.wimy
              {
                home.username = "wimy";
                home.homeDirectory = if pkgs.stdenv.hostPlatform.isDarwin then "/Users/wimy" else "/home/wimy";
                home.stateVersion = "25.05";
                programs.wimy.enable = true;
                programs.wimy.settings = ''
                  terminal "open -na Ghostty"
                '';
                programs.wimy.sketchybar.height = 30;
                programs.wimy.sketchybar.extraConfig = "sketchybar --set clock icon.color=$RED";
              }
            ];
          };
        in
        lib.optionalAttrs (self.packages.${pkgs.stdenv.hostPlatform.system} ? wimy) {
          # the module evaluates and its activation builds
          hm-module = hmCheck.activationPackage;
        }
        // lib.optionalAttrs (pkgs.stdenv.hostPlatform.isDarwin && self.packages.${pkgs.stdenv.hostPlatform.system} ? wimy) {
          # the bundled SketchyBar: bar config installed, wimy's bar-gap
          # matches the bar, the user's settings come after it
          hm-sketchybar = pkgs.runCommand "wimy-hm-sketchybar-check" { } ''
            files=${hmCheck.config.home-files}
            set -x
            grep -qx 'bar-gap 30' $files/.config/wimy/config.kdl
            grep -qx 'terminal "open -na Ghostty"' $files/.config/wimy/config.kdl
            grep -qx 'BAR_HEIGHT=30' $files/.config/sketchybar/settings.sh
            grep -q 'Wimy.app/Contents/MacOS/wimyctl' $files/.config/sketchybar/settings.sh
            grep -qx 'sketchybar --set clock icon.color=$RED' $files/.config/sketchybar/extra.sh
            test -x $files/.config/sketchybar/sketchybarrc
            test -x $files/.config/sketchybar/plugins/wimy.sh
            ${pkgs.bash}/bin/bash -n $files/.config/sketchybar/sketchybarrc
            touch $out
          '';
        }
      );
    };
}
